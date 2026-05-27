package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"runAll/src/domain"
	"runAll/src/infrastructure"
)

type Runner struct {
	cfg                          *Config
	store                        *StatusStore
	levels                       []ExecutionLevel
	processes                    map[string]*exec.Cmd
	mu                           sync.Mutex
	monitors                     map[string]context.CancelFunc
	monitorMu                    sync.Mutex
	logRepository                domain.ServiceLogRepository
	ownershipRepo                domain.ServiceOwnershipRepository
	ownershipGuard               domain.ServiceOwnershipGuardService
	runtimePrereqProbeRepository domain.ServiceRuntimePrereqProbeRepository
	preflightFn                  func(context.Context, Service) error
	listenerPIDsFn               func(string) ([]int, error)
}

const (
	defaultOwnershipSessionID = "runall-bootstrap"
	defaultOwnershipConfigRef = "runtime-managed"
)

type actorSessionContextKey struct{}

// CascadeFailure attaches partial cascade progress to an error for API responses.
type CascadeFailure struct {
	Err    error
	Report domain.CascadeExecutionReport
}

func (e *CascadeFailure) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *CascadeFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type noopRuntimePrereqProbeRepository struct{}

func (noopRuntimePrereqProbeRepository) Probe(service domain.ManagedService) error {
	_ = service
	return nil
}

type runnerRuntimeContextRepository struct {
	runner *Runner
}

func (r *runnerRuntimeContextRepository) FindByName(name string) (domain.ManagedService, error) {
	svc := r.runner.findService(name)
	if svc == nil {
		return domain.ManagedService{}, fmt.Errorf("service %q not found", name)
	}
	status := r.runner.store.Get(name)
	if status == nil {
		return domain.ManagedService{}, fmt.Errorf("service %q not found", name)
	}
	return domain.NewManagedService(svc.Name, status.Group, string(status.Status), svc.DependsOn)
}

func (r *runnerRuntimeContextRepository) Save(service domain.ManagedService) error {
	r.runner.store.Update(service.Name, Status(service.Status), "")
	return nil
}

func (r *runnerRuntimeContextRepository) ListByGroup(groupName string) ([]domain.ManagedService, error) {
	var result []domain.ManagedService
	for _, group := range r.runner.cfg.Groups {
		if group.Name != groupName {
			continue
		}
		for _, svc := range group.Services {
			status := r.runner.store.Get(svc.Name)
			if status == nil {
				continue
			}
			managed, err := domain.NewManagedService(svc.Name, group.Name, string(status.Status), svc.DependsOn)
			if err != nil {
				return nil, err
			}
			result = append(result, managed)
		}
	}
	return result, nil
}

func (r *runnerRuntimeContextRepository) ListAll() ([]domain.ManagedService, error) {
	services := r.runner.cfg.Flatten()
	result := make([]domain.ManagedService, 0, len(services))
	for _, svc := range services {
		status := r.runner.store.Get(svc.Name)
		if status == nil {
			continue
		}
		managed, err := domain.NewManagedService(svc.Name, status.Group, string(status.Status), svc.DependsOn)
		if err != nil {
			return nil, err
		}
		result = append(result, managed)
	}
	return result, nil
}

func NewRunner(cfg *Config, store *StatusStore) (*Runner, error) {
	cfg.fillDefaults()

	services := cfg.Flatten()
	names := make([]string, len(services))
	for i, svc := range services {
		names[i] = svc.Name
	}
	store.Init(names)

	// Populate command and URL in store for UI display
	for _, svc := range services {
		store.SetCommand(svc.Name, svc.Command)
		store.SetURL(svc.Name, svc.HealthCheck.URL)
		store.SetHealthPort(svc.Name, domain.ResolveHealthPort(svc.HealthCheck.URL))
		store.SetCommandPort(svc.Name, domain.ResolveCommandPort(svc.Command))
	}
	for _, group := range cfg.Groups {
		for _, svc := range group.Services {
			store.SetGroup(svc.Name, group.Name)
		}
	}

	// Build dependency status references
	for _, svc := range services {
		deps := make([]DepStatus, len(svc.DependsOn))
		for i, depName := range svc.DependsOn {
			deps[i] = DepStatus{Name: depName, Status: StatusPending}
		}
		store.SetDependsOn(svc.Name, deps)
	}

	levels, err := BuildDAG(services)
	if err != nil {
		return nil, err
	}

	ownershipRepo := infrastructure.NewInMemoryServiceOwnershipRepository()

	return &Runner{
		cfg:                          cfg,
		store:                        store,
		levels:                       levels,
		processes:                    make(map[string]*exec.Cmd),
		monitors:                     make(map[string]context.CancelFunc),
		logRepository:                infrastructure.NewInMemoryServiceLogRepository(infrastructure.DefaultServiceLogCapacity),
		ownershipRepo:                ownershipRepo,
		ownershipGuard:               domain.NewServiceOwnershipGuardService(ownershipRepo),
		runtimePrereqProbeRepository: noopRuntimePrereqProbeRepository{},
		listenerPIDsFn:               listenerPIDs,
	}, nil
}

func withActorSessionID(ctx context.Context, actorSessionID string) context.Context {
	actor := strings.TrimSpace(actorSessionID)
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, actorSessionContextKey{}, actor)
}

func actorSessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(actorSessionContextKey{})
	actor, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(actor)
}

func (r *Runner) Run(ctx context.Context, daemon bool) error {
	for _, level := range r.levels {
		if err := r.executeLevel(ctx, level); err != nil {
			return err
		}
	}

	log.Println("All services healthy.")

	// Start continuous health monitoring for all services.
	services := r.cfg.Flatten()
	for _, svc := range services {
		s := r.store.Get(svc.Name)
		if s != nil && s.Status == StatusHealthy {
			r.startMonitoring(ctx, svc)
		}
	}

	if daemon {
		log.Println("Daemon mode: exiting.")
		return nil
	}

	log.Println("Running. Press Ctrl+C to stop.")
	<-ctx.Done()
	log.Println("Shutting down...")
	r.stopAllMonitors()
	r.Shutdown()
	return nil
}

func (r *Runner) executeLevel(ctx context.Context, level ExecutionLevel) error {
	levelCtx, levelCancel := context.WithCancel(ctx)
	defer levelCancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(level.Services))
	exitFailed := make(chan struct{})
	var exitOnce sync.Once

	for _, node := range level.Services {
		// Check if any dependency was skipped/failed
		skip := false
		for _, depName := range node.Service.DependsOn {
			depStatus := r.store.Get(depName)
			if depStatus.Status == StatusFailed || depStatus.Status == StatusSkipped {
				r.store.Update(node.Service.Name, StatusSkipped, fmt.Sprintf("dependency %s is %s", depName, depStatus.Status))
				log.Printf("[%s] SKIPPED: dependency %s is %s", node.Service.Name, depName, depStatus.Status)
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		wg.Add(1)
		go func(node *ServiceNode) {
			defer wg.Done()
			select {
			case <-exitFailed:
				return
			default:
			}
			if err := r.runPreflight(levelCtx, node.Service); err != nil {
				current := r.store.Get(node.Service.Name)
				if current == nil || current.Status != StatusFailed {
					r.store.Update(node.Service.Name, StatusFailed, fmt.Sprintf("preflight failed: %v", err))
				}
				r.store.UpdateDependencyStatus(node.Service.Name, StatusFailed)
				errCh <- err
				exitOnce.Do(func() { close(exitFailed); levelCancel() })
				return
			}
			if err := r.startAndCheck(levelCtx, node); err != nil {
				errCh <- err
				exitOnce.Do(func() { close(exitFailed); levelCancel() })
			}
		}(node)
	}

	wg.Wait()
	close(errCh)

	// Collect errors
	var firstErr error
	for err := range errCh {
		if firstErr == nil {
			firstErr = err
		}
	}

	// If any exit-type failure occurred, stop everything
	if firstErr != nil {
		r.Shutdown()
		return firstErr
	}

	return nil
}

func (r *Runner) runPreflight(ctx context.Context, svc Service) error {
	if r.preflightFn != nil {
		return r.preflightFn(ctx, svc)
	}
	if err := r.preflightService(ctx, svc); err != nil {
		return err
	}
	return r.probeRuntimePrerequisite(svc)
}

func (r *Runner) probeRuntimePrerequisite(svc Service) error {
	if r.runtimePrereqProbeRepository == nil {
		return nil
	}

	status := string(StatusPending)
	if current := r.store.Get(svc.Name); current != nil {
		status = string(current.Status)
	}
	managedService, err := domain.NewManagedService(svc.Name, "", status, svc.DependsOn)
	if err != nil {
		return fmt.Errorf("[%s] build runtime prereq context: %w", svc.Name, err)
	}

	if err := r.runtimePrereqProbeRepository.Probe(managedService); err != nil {
		msg := fmt.Sprintf("%s: %v", domain.ServiceFailureCodeRuntimePrereq, err)
		r.store.RecordPreflightFailure(svc.Name, domain.ServiceFailureCodeRuntimePrereq, msg)
		return fmt.Errorf("[%s] %s", svc.Name, msg)
	}
	return nil
}

func (r *Runner) startAndCheck(ctx context.Context, node *ServiceNode) error {
	svc := node.Service
	r.store.Update(svc.Name, StatusStarting, "")

	cmd := exec.Command("sh", "-c", svc.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if svc.WorkingDir != "" {
		cmd.Dir = svc.WorkingDir
	}
	if len(svc.Env) > 0 {
		env := os.Environ()
		for k, v := range svc.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.store.RecordFailure(
			svc.Name,
			domain.ServiceLifecyclePhaseLaunch,
			domain.ServiceFailureCodeProcessExited,
			err.Error(),
		)
		r.store.UpdateDependencyStatus(svc.Name, StatusFailed)
		if svc.OnFailure == "exit" {
			return fmt.Errorf("[%s] stdout pipe: %w", svc.Name, err)
		}
		log.Printf("[%s] stdout pipe error, skipping: %v", svc.Name, err)
		return nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.store.RecordFailure(
			svc.Name,
			domain.ServiceLifecyclePhaseLaunch,
			domain.ServiceFailureCodeProcessExited,
			err.Error(),
		)
		r.store.UpdateDependencyStatus(svc.Name, StatusFailed)
		if svc.OnFailure == "exit" {
			return fmt.Errorf("[%s] stderr pipe: %w", svc.Name, err)
		}
		log.Printf("[%s] stderr pipe error, skipping: %v", svc.Name, err)
		return nil
	}

	if err := cmd.Start(); err != nil {
		r.store.RecordFailure(
			svc.Name,
			domain.ServiceLifecyclePhaseLaunch,
			domain.ServiceFailureCodeProcessExited,
			err.Error(),
		)
		r.store.UpdateDependencyStatus(svc.Name, StatusFailed)
		if svc.OnFailure == "exit" {
			return fmt.Errorf("[%s] failed to start: %w", svc.Name, err)
		}
		log.Printf("[%s] failed to start, skipping: %v", svc.Name, err)
		return nil
	}

	r.mu.Lock()
	r.processes[svc.Name] = cmd
	r.mu.Unlock()
	r.store.SetPID(svc.Name, cmd.Process.Pid)

	go streamOutput(stdout, svc.Name, domain.StreamStdout, r.logRepository)
	go streamOutput(stderr, svc.Name, domain.StreamStderr, r.logRepository)

	// Health check
	r.store.Update(svc.Name, StatusRetrying, "")
	err = waitHealthyWithLaunchCheck(ctx, cmd.Process, svc.HealthCheck)
	if err != nil {
		phase, code := classifyStartupFailure(err)
		if phase == "" || code == "" {
			r.store.Update(svc.Name, StatusFailed, err.Error())
		} else {
			r.store.RecordFailure(svc.Name, phase, code, err.Error())
		}
		r.store.UpdateDependencyStatus(svc.Name, StatusFailed)
		log.Printf("[%s] health check failed: %v", svc.Name, err)
		if svc.OnFailure == "exit" {
			return fmt.Errorf("[%s] health check failed: %w", svc.Name, err)
		}
		return nil
	}

	r.store.Update(svc.Name, StatusHealthy, "")
	r.store.UpdateDependencyStatus(svc.Name, StatusHealthy)
	r.establishServiceOwnership(svc, cmd.Process.Pid, actorSessionIDFromContext(ctx))
	log.Printf("[%s] healthy (%s)", svc.Name, svc.HealthCheck.URL)
	return nil
}

func (r *Runner) establishServiceOwnership(svc Service, pid int, actorSessionID string) {
	if r.ownershipRepo == nil || pid <= 0 {
		return
	}
	owner := strings.TrimSpace(actorSessionID)
	if owner == "" {
		owner = defaultOwnershipSessionID
	}
	ownership, err := domain.NewServiceOwnership(
		svc.Name,
		owner,
		pid,
		defaultOwnershipConfigRef,
		svc.HealthCheck.URL,
		time.Now(),
	)
	if err != nil {
		log.Printf("[%s] establish ownership skipped: %v", svc.Name, err)
		return
	}
	if err := r.ownershipRepo.Save(ownership); err != nil {
		log.Printf("[%s] persist ownership skipped: %v", svc.Name, err)
	}
}

func waitHealthyWithLaunchCheck(ctx context.Context, process *os.Process, cfg HealthCheck) error {
	interval := time.Duration(cfg.Backoff.Initial * float64(time.Second))
	maxInterval := time.Duration(cfg.Backoff.Max * float64(time.Second))
	deadline := time.Now().Add(time.Duration(cfg.Timeout) * time.Second)
	var lastCheckErr error

	for i := 0; i < cfg.Retries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}

		if processHasExited(process) {
			return fmt.Errorf("launch process exited before readiness")
		}

		if time.Now().After(deadline) {
			if lastCheckErr != nil {
				return fmt.Errorf("health check timed out after %ds (last error: %v)", cfg.Timeout, lastCheckErr)
			}
			return fmt.Errorf("health check timed out after %ds", cfg.Timeout)
		}

		err := checkHealth(ctx, cfg.URL)
		if err == nil {
			return nil
		}
		lastCheckErr = err
		if processHasExited(process) {
			return fmt.Errorf("launch process exited before readiness")
		}

		interval = time.Duration(float64(interval) * cfg.Backoff.Multiplier)
		if interval > maxInterval {
			interval = maxInterval
		}
	}

	if lastCheckErr != nil {
		return fmt.Errorf("health check failed after %d retries: %w", cfg.Retries, lastCheckErr)
	}
	return fmt.Errorf("health check failed after %d retries", cfg.Retries)
}

func processHasExited(process *os.Process) bool {
	if process == nil {
		return true
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return true
	}
	return false
}

func classifyStartupFailure(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", ""
	}
	msg := err.Error()
	if strings.Contains(msg, "launch process exited before readiness") {
		return domain.ServiceLifecyclePhaseLaunch, domain.ServiceFailureCodeProcessExited
	}
	if strings.Contains(msg, "unhealthy: HTTP") {
		return domain.ServiceLifecyclePhaseReadiness, domain.ServiceFailureCodeBadReadiness
	}
	return domain.ServiceLifecyclePhaseReadiness, domain.ServiceFailureCodeReadinessTimeout
}

func (r *Runner) Shutdown() {
	r.stopAllMonitors()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Shutdown in reverse order
	for i := len(r.levels) - 1; i >= 0; i-- {
		level := r.levels[i]
		var wg sync.WaitGroup
		for _, node := range level.Services {
			cmd, ok := r.processes[node.Service.Name]
			if !ok || cmd.Process == nil {
				continue
			}
			wg.Add(1)
			go func(name string, cmd *exec.Cmd) {
				defer wg.Done()
				log.Printf("[%s] sending SIGTERM", name)
				syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

				done := make(chan struct{})
				go func() {
					cmd.Wait()
					close(done)
				}()
				select {
				case <-done:
					log.Printf("[%s] stopped", name)
				case <-time.After(5 * time.Second):
					log.Printf("[%s] did not stop, sending SIGKILL", name)
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					cmd.Wait()
				}
			}(node.Service.Name, cmd)
		}
		wg.Wait()
	}
}

func (r *Runner) RestartService(ctx context.Context, name string) error {
	return r.restartService(ctx, name)
}

func (r *Runner) TakeoverService(name string, actorSessionID string) error {
	actor := strings.TrimSpace(actorSessionID)
	if actor == "" {
		return fmt.Errorf("actor session id is required for explicit takeover")
	}

	svc := r.findService(name)
	if svc == nil {
		return fmt.Errorf("service %q not found", name)
	}
	current := r.store.Get(name)
	if current == nil {
		return fmt.Errorf("service %q not found", name)
	}
	existingOwnership, err := r.ownershipRepo.FindByServiceName(name)
	if err != nil {
		return err
	}
	pid := current.PID
	if pid <= 0 {
		pid = r.discoverRunningPIDFromListeners(svc)
	}
	usedResidualOwnershipForStoppedOrFailed := pid <= 0 &&
		(current.Status == StatusStopped || current.Status == StatusFailed) &&
		existingOwnership.ServiceName != "" && existingOwnership.PID > 0
	if usedResidualOwnershipForStoppedOrFailed {
		pid = existingOwnership.PID
	}
	if pid <= 0 {
		return fmt.Errorf("service %q has no running process to take over", name)
	}

	ownership, err := domain.NewServiceOwnership(
		name,
		actor,
		pid,
		"takeover",
		svc.HealthCheck.URL,
		time.Now(),
	)
	if err != nil {
		return err
	}
	if err := r.ownershipRepo.Save(ownership); err != nil {
		return err
	}
	if usedResidualOwnershipForStoppedOrFailed && current.Status == StatusFailed {
		r.store.Update(name, StatusStopped, "")
		r.store.UpdateDependencyStatus(name, StatusStopped)
	}
	return nil
}

func (r *Runner) discoverRunningPIDFromListeners(svc *Service) int {
	if svc == nil {
		return 0
	}
	lookup := r.listenerPIDsFn
	if lookup == nil {
		lookup = listenerPIDs
	}
	var candidates []int
	for _, port := range resolveServicePorts(svc) {
		pids, err := lookup(port)
		if err != nil {
			continue
		}
		for _, pid := range pids {
			if pid > 0 {
				candidates = append(candidates, pid)
			}
		}
	}
	if len(candidates) == 0 {
		return 0
	}
	sort.Ints(candidates)
	return candidates[0]
}

func (r *Runner) RestartServiceWithActor(ctx context.Context, name string, actorSessionID string) error {
	if err := r.ownershipGuard.EnsureOperableBySession(name, actorSessionID); err != nil {
		return err
	}
	return r.restartService(withActorSessionID(ctx, actorSessionID), name)
}

func (r *Runner) restartService(ctx context.Context, name string) error {
	svc := r.findService(name)
	if svc == nil {
		return fmt.Errorf("service %q not found", name)
	}

	previousStatus := StatusFailed
	if r.store.CompareAndSwapStatus(name, StatusHealthy, StatusRestarting) {
		previousStatus = StatusHealthy
	} else if r.store.CompareAndSwapStatus(name, StatusFailed, StatusRestarting) {
		previousStatus = StatusFailed
	} else if r.store.CompareAndSwapStatus(name, StatusStopped, StatusRestarting) {
		previousStatus = StatusStopped
	} else if r.store.CompareAndSwapStatus(name, StatusPending, StatusRestarting) {
		previousStatus = StatusPending
	} else {
		current := r.store.Get(name)
		if current == nil {
			return fmt.Errorf("service %q not found", name)
		}
		return fmt.Errorf("service %q is %s, can only restart healthy, failed, stopped, or pending services", name, current.Status)
	}
	r.stopMonitoring(name)

	// Build before stopping: a failed build leaves the old process running.
	if svc.BuildCommand != "" {
		r.store.Update(name, StatusBuilding, "")
		log.Printf("[%s] building...", name)
		if err := r.runBuild(ctx, svc); err != nil {
			r.store.Update(name, previousStatus, err.Error())
			return err
		}
	}

	// Stop existing process
	_ = r.stopProcess(name)

	// Start and health check
	node := &ServiceNode{Service: *svc}
	if err := r.startAndCheck(ctx, node); err != nil {
		r.stopMonitoring(name)
		_ = r.stopProcess(name)
		r.store.SetPID(name, 0)
		return err
	}
	if err := r.ensureHealthyAfterManualStart(name); err != nil {
		return err
	}

	// Resume continuous monitoring
	r.startMonitoring(ctx, *svc)
	return nil
}

func (r *Runner) StopService(ctx context.Context, name string) error {
	return r.stopService(ctx, name)
}

func (r *Runner) StopServiceWithActor(ctx context.Context, name string, actorSessionID string) error {
	if err := r.ownershipGuard.EnsureOperableBySession(name, actorSessionID); err != nil {
		return err
	}
	return r.stopService(ctx, name)
}

func (r *Runner) stopService(ctx context.Context, name string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	svc := r.findService(name)
	if svc == nil {
		return fmt.Errorf("service %q not found", name)
	}

	current := r.store.Get(name)
	if current == nil {
		return fmt.Errorf("service %q not found", name)
	}
	if current.Status == StatusStopped {
		return nil
	}

	managedService, err := domain.NewManagedService(svc.Name, current.Group, string(current.Status), svc.DependsOn)
	if err != nil {
		return err
	}
	policyService := domain.NewServiceStopPolicyService(&runnerRuntimeContextRepository{runner: r})
	decision, err := policyService.EvaluateStop(name)
	if err != nil {
		return err
	}
	activeDependents := mergeDependentNames(decision.ActiveDependents, r.runningDependents(name))
	if err := managedService.CanStop(activeDependents); err != nil {
		return err
	}

	r.stopMonitoring(name)
	hadTrackedProcess := r.stopProcess(name)

	if !hadTrackedProcess {
		if err := r.ensureServiceNotReachable(svc); err != nil {
			r.store.Update(name, StatusFailed, err.Error())
			return err
		}
	}

	r.store.SetPID(name, 0)
	r.store.Update(name, StatusStopped, "")
	r.store.UpdateDependencyStatus(name, StatusStopped)
	if r.ownershipRepo != nil {
		if err := r.ownershipRepo.DeleteByServiceName(name); err != nil {
			cleanupErr := fmt.Errorf("ownership cleanup failed: %w", err)
			r.store.Update(name, StatusFailed, cleanupErr.Error())
			r.store.UpdateDependencyStatus(name, StatusFailed)
			return cleanupErr
		}
	}
	return nil
}

func (r *Runner) StartService(ctx context.Context, name string) error {
	return r.startService(ctx, name)
}

func (r *Runner) StartServiceWithActor(ctx context.Context, name string, actorSessionID string) error {
	actor := strings.TrimSpace(actorSessionID)
	if actor == "" {
		return fmt.Errorf("actor session id is required")
	}
	if err := r.ownershipGuard.EnsureOperableBySession(name, actor); err != nil {
		return err
	}
	return r.startService(withActorSessionID(ctx, actor), name)
}

func (r *Runner) cascadeOrchestration() *domain.ServiceCascadeOrchestrationService {
	return domain.NewServiceCascadeOrchestrationService(
		newConfigServiceTopologyRepository(r.cfg),
		&runnerRuntimeContextRepository{runner: r},
	)
}

func (r *Runner) StartServiceCascade(ctx context.Context, name string) error {
	return r.StartServiceCascadeWithActor(ctx, name, defaultOwnershipSessionID)
}

func (r *Runner) StartServiceCascadeWithActor(ctx context.Context, name string, actorSessionID string) error {
	actor := strings.TrimSpace(actorSessionID)
	if actor == "" {
		return fmt.Errorf("actor session id is required")
	}
	plan, err := r.cascadeOrchestration().PlanStartCascade(name)
	if err != nil {
		return err
	}
	if len(plan.OrderedNames) == 0 {
		current := r.store.Get(name)
		if current != nil && current.Status == StatusHealthy {
			return nil
		}
		return fmt.Errorf("no stopped services to start in cascade for %q", name)
	}
	log.Printf("[cascade] start plan: %s", plan.String())
	return r.executeLifecyclePlan(ctx, plan, actor, func(stepCtx context.Context, serviceName, stepActor string) error {
		return r.StartServiceWithActor(stepCtx, serviceName, stepActor)
	})
}

func (r *Runner) StopServiceCascade(ctx context.Context, name string) error {
	return r.StopServiceCascadeWithActor(ctx, name, defaultOwnershipSessionID)
}

func (r *Runner) StopServiceCascadeWithActor(ctx context.Context, name string, actorSessionID string) error {
	actor := strings.TrimSpace(actorSessionID)
	if actor == "" {
		return fmt.Errorf("actor session id is required")
	}
	plan, err := r.cascadeOrchestration().PlanStopCascade(name)
	if err != nil {
		return err
	}
	if len(plan.OrderedNames) == 0 {
		return nil
	}
	log.Printf("[cascade] stop plan: %s", plan.String())
	return r.executeLifecyclePlan(ctx, plan, actor, func(stepCtx context.Context, serviceName, stepActor string) error {
		return r.StopServiceWithActor(stepCtx, serviceName, stepActor)
	})
}

func (r *Runner) StartGroup(ctx context.Context, group string) error {
	return r.StartGroupWithActor(ctx, group, defaultOwnershipSessionID)
}

func (r *Runner) StartGroupWithActor(ctx context.Context, group string, actorSessionID string) error {
	actor := strings.TrimSpace(actorSessionID)
	if actor == "" {
		return fmt.Errorf("actor session id is required")
	}
	plan, err := r.cascadeOrchestration().PlanStartGroup(group)
	if err != nil {
		return err
	}
	if len(plan.OrderedNames) == 0 {
		return nil
	}
	log.Printf("[cascade] start group %q plan: %s", group, plan.String())
	return r.executeLifecyclePlan(ctx, plan, actor, func(stepCtx context.Context, serviceName, stepActor string) error {
		return r.StartServiceWithActor(stepCtx, serviceName, stepActor)
	})
}

func (r *Runner) executeLifecyclePlan(
	ctx context.Context,
	plan domain.ServiceLifecyclePlan,
	actor string,
	stepFn func(context.Context, string, string) error,
) error {
	completed := make([]string, 0, len(plan.OrderedNames))
	for _, serviceName := range plan.OrderedNames {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := stepFn(ctx, serviceName, actor); err != nil {
			if len(completed) == 0 {
				return err
			}
			report, reportErr := domain.NewCascadeExecutionReport(completed, serviceName)
			if reportErr != nil {
				return fmt.Errorf("%s cascade failed on %q: %w", plan.Operation, serviceName, err)
			}
			wrapped := fmt.Errorf("%s cascade failed on %q: %w", plan.Operation, serviceName, err)
			return &CascadeFailure{Err: wrapped, Report: report}
		}
		completed = append(completed, serviceName)
	}
	return nil
}

func (r *Runner) startService(ctx context.Context, name string) error {
	svc := r.findService(name)
	if svc == nil {
		return fmt.Errorf("service %q not found", name)
	}
	current := r.store.Get(name)
	if current == nil {
		return fmt.Errorf("service %q not found", name)
	}

	managedService, err := domain.NewManagedService(svc.Name, current.Group, string(current.Status), svc.DependsOn)
	if err != nil {
		return err
	}
	if !managedService.CanStart() {
		return fmt.Errorf("service %q is %s, can only start stopped services", name, current.Status)
	}

	if !r.store.CompareAndSwapStatus(name, StatusStopped, StatusStarting) {
		latest := r.store.Get(name)
		if latest == nil {
			return fmt.Errorf("service %q not found", name)
		}
		return fmt.Errorf("service %q is %s, can only start stopped services", name, latest.Status)
	}

	if err := r.runPreflight(ctx, *svc); err != nil {
		r.store.SetPID(name, 0)
		return err
	}

	node := &ServiceNode{Service: *svc}
	if err := r.startAndCheck(ctx, node); err != nil {
		r.stopMonitoring(name)
		_ = r.stopProcess(name)
		r.store.SetPID(name, 0)
		return err
	}
	if err := r.ensureHealthyAfterManualStart(name); err != nil {
		return err
	}

	r.startMonitoring(ctx, *svc)
	return nil
}

func (r *Runner) ensureHealthyAfterManualStart(name string) error {
	current := r.store.Get(name)
	if current == nil {
		return fmt.Errorf("service %q not found", name)
	}
	if current.Status == StatusHealthy {
		return nil
	}
	r.stopMonitoring(name)
	_ = r.stopProcess(name)
	r.store.SetPID(name, 0)
	if current.Error != "" {
		return fmt.Errorf("service %q failed to start: %s", name, current.Error)
	}
	return fmt.Errorf("service %q failed to start", name)
}

func (r *Runner) runningDependents(name string) []string {
	var running []string
	for _, svc := range r.cfg.Flatten() {
		if !dependsOnService(svc.DependsOn, name) {
			continue
		}
		status := r.store.Get(svc.Name)
		if status != nil && status.PID > 0 {
			running = append(running, svc.Name)
		}
	}
	return running
}

func dependsOnService(dependsOn []string, name string) bool {
	for _, dep := range dependsOn {
		if strings.TrimSpace(dep) == name {
			return true
		}
	}
	return false
}

func mergeDependentNames(parts ...[]string) []string {
	seen := make(map[string]struct{})
	for _, list := range parts {
		for _, item := range list {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			seen[item] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for item := range seen {
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

func (r *Runner) StopGroup(ctx context.Context, group string) error {
	return r.stopGroup(ctx, group, func(stopCtx context.Context, serviceName string) error {
		return r.StopService(stopCtx, serviceName)
	})
}

func (r *Runner) StopGroupWithActor(ctx context.Context, group string, actorSessionID string) error {
	actor := strings.TrimSpace(actorSessionID)
	if actor == "" {
		return fmt.Errorf("actor session id is required")
	}
	return r.stopGroup(ctx, group, func(stopCtx context.Context, serviceName string) error {
		return r.StopServiceWithActor(stopCtx, serviceName, actor)
	})
}

func (r *Runner) stopGroup(ctx context.Context, group string, stopFn func(context.Context, string) error) error {
	var groupServices []Service
	for _, g := range r.cfg.Groups {
		if g.Name == group {
			groupServices = g.Services
			break
		}
	}
	if groupServices == nil {
		return fmt.Errorf("group %q not found", group)
	}

	stopOrder, err := stopOrderForGroup(groupServices)
	if err != nil {
		return fmt.Errorf("group %q has invalid dependencies: %w", group, err)
	}

	for _, serviceName := range stopOrder {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := stopFn(ctx, serviceName); err != nil {
			return fmt.Errorf("stop group %q failed on %q: %w", group, serviceName, err)
		}
	}
	return nil
}

func (r *Runner) BuildService(ctx context.Context, name string) error {
	svc := r.findService(name)
	if svc == nil {
		return fmt.Errorf("service %q not found", name)
	}
	if svc.BuildCommand == "" {
		return fmt.Errorf("service %q has no build command configured", name)
	}

	previousStatus := StatusFailed
	if r.store.CompareAndSwapStatus(name, StatusHealthy, StatusBuilding) {
		previousStatus = StatusHealthy
	} else if r.store.CompareAndSwapStatus(name, StatusFailed, StatusBuilding) {
		previousStatus = StatusFailed
	} else if r.store.CompareAndSwapStatus(name, StatusStopped, StatusBuilding) {
		previousStatus = StatusStopped
	} else {
		current := r.store.Get(name)
		if current == nil {
			return fmt.Errorf("service %q not found", name)
		}
		return fmt.Errorf("service %q is %s, can only build healthy, failed, or stopped services", name, current.Status)
	}

	log.Printf("[%s] build-only requested", name)

	if err := r.runBuild(ctx, svc); err != nil {
		r.store.Update(name, StatusFailed, err.Error())
		return err
	}

	r.store.Update(name, previousStatus, "")
	return nil
}

func (r *Runner) findService(name string) *Service {
	for _, g := range r.cfg.Groups {
		for _, svc := range g.Services {
			if svc.Name == name {
				return &svc
			}
		}
	}
	return nil
}

func (r *Runner) stopProcess(name string) bool {
	r.mu.Lock()
	cmd, ok := r.processes[name]
	if ok {
		delete(r.processes, name)
	}
	r.mu.Unlock()

	if !ok || cmd == nil || cmd.Process == nil {
		return false
	}

	log.Printf("[%s] restarting: sending SIGTERM", name)
	syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		log.Printf("[%s] stopped for restart", name)
	case <-time.After(5 * time.Second):
		log.Printf("[%s] did not stop, sending SIGKILL", name)
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Wait()
	}
	return true
}

func (r *Runner) ensureServiceNotReachable(svc *Service) error {
	if svc == nil {
		return fmt.Errorf("service is required")
	}
	if checkHealth(context.Background(), svc.HealthCheck.URL) != nil {
		return nil
	}

	ports := resolveServicePorts(svc)
	if len(ports) == 0 {
		return fmt.Errorf("service %q still reachable at %s after stop", svc.Name, svc.HealthCheck.URL)
	}

	for _, port := range ports {
		if err := terminateListenersByPort(port); err != nil {
			log.Printf("[%s] stop fallback on port %s failed: %v", svc.Name, port, err)
		}
		if checkHealth(context.Background(), svc.HealthCheck.URL) != nil {
			return nil
		}
	}

	return fmt.Errorf("service %q still reachable at %s after stop", svc.Name, svc.HealthCheck.URL)
}

func resolveServicePorts(svc *Service) []string {
	seen := map[string]struct{}{}
	var ports []string

	for _, candidate := range []string{
		domain.ResolveHealthPort(svc.HealthCheck.URL),
		domain.ResolveCommandPort(svc.Command),
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		ports = append(ports, candidate)
	}

	return ports
}

func terminateListenersByPort(port string) error {
	pids, err := listenerPIDs(port)
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		return nil
	}

	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	time.Sleep(250 * time.Millisecond)

	remaining := make([]int, 0, len(pids))
	for _, pid := range pids {
		if err := syscall.Kill(pid, 0); err == nil {
			remaining = append(remaining, pid)
		}
	}
	for _, pid := range remaining {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}

	return nil
}

func listenerPIDs(port string) ([]int, error) {
	if strings.TrimSpace(port) == "" {
		return nil, nil
	}

	cmd := exec.Command("lsof", "-t", "-iTCP:"+port, "-sTCP:LISTEN")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("list listeners on port %s: %w", port, err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	pids := make([]int, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, convErr := strconv.Atoi(line)
		if convErr != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func stopOrderForGroup(services []Service) ([]string, error) {
	if len(services) == 0 {
		return nil, nil
	}

	servicesByName := make(map[string]Service, len(services))
	for _, svc := range services {
		servicesByName[svc.Name] = svc
	}

	visitState := make(map[string]int, len(services))
	order := make([]string, 0, len(services))

	var visit func(string) error
	visit = func(name string) error {
		switch visitState[name] {
		case 1:
			return fmt.Errorf("cyclic dependency detected at service %q", name)
		case 2:
			return nil
		}

		visitState[name] = 1
		svc := servicesByName[name]
		for _, dep := range svc.DependsOn {
			if _, exists := servicesByName[dep]; !exists {
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		visitState[name] = 2
		order = append(order, name)
		return nil
	}

	for _, svc := range services {
		if err := visit(svc.Name); err != nil {
			return nil, err
		}
	}

	stopOrder := make([]string, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		stopOrder = append(stopOrder, order[i])
	}
	return stopOrder, nil
}

func (r *Runner) runBuild(ctx context.Context, svc *Service) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", svc.BuildCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if svc.WorkingDir != "" {
		cmd.Dir = svc.WorkingDir
	}
	if len(svc.Env) > 0 {
		env := os.Environ()
		for k, v := range svc.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("[%s] build stdout pipe: %w", svc.Name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("[%s] build stderr pipe: %w", svc.Name, err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("[%s] build failed to start: %w", svc.Name, err)
	}

	go streamOutput(stdout, svc.Name, domain.StreamStdout, r.logRepository)
	go streamOutput(stderr, svc.Name, domain.StreamStderr, r.logRepository)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("[%s] build failed: %w", svc.Name, err)
	}

	log.Printf("[%s] build succeeded", svc.Name)
	return nil
}

func streamOutput(reader io.Reader, name, stream string, repository domain.ServiceLogRepository) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if repository != nil {
			entry, err := domain.NewLogEntry(time.Now(), name, stream, line)
			if err != nil {
				log.Printf("[%s] log entry skipped: %v", name, err)
				continue
			}
			repository.Append(name, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[%s] output read error: %v", name, err)
	}
}

func (r *Runner) startMonitoring(ctx context.Context, svc Service) {
	r.monitorMu.Lock()
	if _, exists := r.monitors[svc.Name]; exists {
		r.monitorMu.Unlock()
		return
	}
	monCtx, cancel := context.WithCancel(ctx)
	r.monitors[svc.Name] = cancel
	r.monitorMu.Unlock()

	interval := time.Duration(svc.HealthCheck.CheckInterval) * time.Second
	go r.runMonitor(monCtx, svc, interval)
}

func (r *Runner) stopMonitoring(name string) {
	r.monitorMu.Lock()
	defer r.monitorMu.Unlock()
	if cancel, ok := r.monitors[name]; ok {
		cancel()
		delete(r.monitors, name)
	}
}

func (r *Runner) stopAllMonitors() {
	r.monitorMu.Lock()
	defer r.monitorMu.Unlock()
	for name, cancel := range r.monitors {
		cancel()
		delete(r.monitors, name)
	}
}

func (r *Runner) removeMonitor(name string) {
	r.monitorMu.Lock()
	defer r.monitorMu.Unlock()
	delete(r.monitors, name)
}

func (r *Runner) runMonitor(ctx context.Context, svc Service, interval time.Duration) {
	defer r.removeMonitor(svc.Name)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutive := 0
	threshold := svc.HealthCheck.UnhealthyThreshold

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := checkHealth(ctx, svc.HealthCheck.URL)
			r.store.SetLastChecked(svc.Name, time.Now())
			if err != nil {
				consecutive++
				log.Printf("[%s] health check failed (%d/%d): %v", svc.Name, consecutive, threshold, err)
				if consecutive >= threshold {
					r.store.Update(svc.Name, StatusFailed, err.Error())
					log.Printf("[%s] marked failed after %d consecutive failures", svc.Name, consecutive)
					return
				}
			} else {
				consecutive = 0
			}
		}
	}
}
