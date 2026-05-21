package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"runAll/src/domain"
	"runAll/src/infrastructure"
)

type Runner struct {
	cfg           *Config
	store         *StatusStore
	levels        []ExecutionLevel
	processes     map[string]*exec.Cmd
	mu            sync.Mutex
	monitors      map[string]context.CancelFunc
	monitorMu     sync.Mutex
	logRepository domain.ServiceLogRepository
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

	return &Runner{
		cfg:           cfg,
		store:         store,
		levels:        levels,
		processes:     make(map[string]*exec.Cmd),
		monitors:      make(map[string]context.CancelFunc),
		logRepository: infrastructure.NewInMemoryServiceLogRepository(infrastructure.DefaultServiceLogCapacity),
	}, nil
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
		r.store.Update(svc.Name, StatusFailed, err.Error())
		r.store.UpdateDependencyStatus(svc.Name, StatusFailed)
		if svc.OnFailure == "exit" {
			return fmt.Errorf("[%s] stdout pipe: %w", svc.Name, err)
		}
		log.Printf("[%s] stdout pipe error, skipping: %v", svc.Name, err)
		return nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.store.Update(svc.Name, StatusFailed, err.Error())
		r.store.UpdateDependencyStatus(svc.Name, StatusFailed)
		if svc.OnFailure == "exit" {
			return fmt.Errorf("[%s] stderr pipe: %w", svc.Name, err)
		}
		log.Printf("[%s] stderr pipe error, skipping: %v", svc.Name, err)
		return nil
	}

	if err := cmd.Start(); err != nil {
		r.store.Update(svc.Name, StatusFailed, err.Error())
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
	err = waitHealthy(ctx, svc.HealthCheck)
	if err != nil {
		r.store.Update(svc.Name, StatusFailed, err.Error())
		r.store.UpdateDependencyStatus(svc.Name, StatusFailed)
		log.Printf("[%s] health check failed: %v", svc.Name, err)
		if svc.OnFailure == "exit" {
			return fmt.Errorf("[%s] health check failed: %w", svc.Name, err)
		}
		return nil
	}

	r.store.Update(svc.Name, StatusHealthy, "")
	r.store.UpdateDependencyStatus(svc.Name, StatusHealthy)
	log.Printf("[%s] healthy (%s)", svc.Name, svc.HealthCheck.URL)
	return nil
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
	svc := r.findService(name)
	if svc == nil {
		return fmt.Errorf("service %q not found", name)
	}

	previousStatus := StatusFailed
	if r.store.CompareAndSwapStatus(name, StatusHealthy, StatusRestarting) {
		previousStatus = StatusHealthy
	} else if !r.store.CompareAndSwapStatus(name, StatusFailed, StatusRestarting) {
		current := r.store.Get(name)
		if current == nil {
			return fmt.Errorf("service %q not found", name)
		}
		return fmt.Errorf("service %q is %s, can only restart healthy or failed services", name, current.Status)
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
	r.stopProcess(name)

	// Start and health check
	node := &ServiceNode{Service: *svc}
	if err := r.startAndCheck(ctx, node); err != nil {
		r.stopMonitoring(name)
		r.stopProcess(name)
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
	r.stopProcess(name)
	r.store.SetPID(name, 0)
	r.store.Update(name, StatusStopped, "")
	r.store.UpdateDependencyStatus(name, StatusStopped)
	return nil
}

func (r *Runner) StartService(ctx context.Context, name string) error {
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

	node := &ServiceNode{Service: *svc}
	if err := r.startAndCheck(ctx, node); err != nil {
		r.stopMonitoring(name)
		r.stopProcess(name)
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
	r.stopProcess(name)
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
		if err := r.StopService(ctx, serviceName); err != nil {
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
	} else if !r.store.CompareAndSwapStatus(name, StatusFailed, StatusBuilding) {
		current := r.store.Get(name)
		if current == nil {
			return fmt.Errorf("service %q not found", name)
		}
		return fmt.Errorf("service %q is %s, can only build healthy or failed services", name, current.Status)
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

func (r *Runner) stopProcess(name string) {
	r.mu.Lock()
	cmd, ok := r.processes[name]
	if ok {
		delete(r.processes, name)
	}
	r.mu.Unlock()

	if !ok || cmd == nil || cmd.Process == nil {
		return
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
