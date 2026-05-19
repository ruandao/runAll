package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type Runner struct {
	cfg       *Config
	store     *StatusStore
	levels    []ExecutionLevel
	processes map[string]*exec.Cmd
	mu        sync.Mutex
}

func NewRunner(cfg *Config, store *StatusStore) (*Runner, error) {
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
		cfg:       cfg,
		store:     store,
		levels:    levels,
		processes: make(map[string]*exec.Cmd),
	}, nil
}

func (r *Runner) Run(ctx context.Context, daemon bool) error {
	for _, level := range r.levels {
		if err := r.executeLevel(ctx, level); err != nil {
			return err
		}
	}

	log.Println("All services healthy.")

	if daemon {
		log.Println("Daemon mode: exiting.")
		return nil
	}

	log.Println("Running. Press Ctrl+C to stop.")
	<-ctx.Done()
	log.Println("Shutting down...")
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

	cmd := exec.CommandContext(ctx, "sh", "-c", svc.Command)
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
		if svc.OnFailure == "exit" {
			return fmt.Errorf("[%s] stdout pipe: %w", svc.Name, err)
		}
		log.Printf("[%s] stdout pipe error, skipping: %v", svc.Name, err)
		return nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.store.Update(svc.Name, StatusFailed, err.Error())
		if svc.OnFailure == "exit" {
			return fmt.Errorf("[%s] stderr pipe: %w", svc.Name, err)
		}
		log.Printf("[%s] stderr pipe error, skipping: %v", svc.Name, err)
		return nil
	}

	if err := cmd.Start(); err != nil {
		r.store.Update(svc.Name, StatusFailed, err.Error())
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

	go streamOutput(stdout, svc.Name)
	go streamOutput(stderr, svc.Name)

	// Health check
	r.store.Update(svc.Name, StatusRetrying, "")
	err = waitHealthy(ctx, svc.HealthCheck)
	if err != nil {
		r.store.Update(svc.Name, StatusFailed, err.Error())
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

	current := r.store.Get(name)
	if current == nil {
		return fmt.Errorf("service %q not found", name)
	}
	if current.Status != StatusHealthy && current.Status != StatusFailed {
		return fmt.Errorf("service %q is %s, can only restart healthy or failed services", name, current.Status)
	}

	r.store.Update(name, StatusRestarting, "")

	// Stop existing process
	r.stopProcess(name)

	// Start and health check
	node := &ServiceNode{Service: *svc}
	if err := r.startAndCheck(ctx, node); err != nil {
		return err
	}

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

func streamOutput(r io.Reader, name string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		log.Printf("[%s] %s", name, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[%s] output read error: %v", name, err)
	}
}
