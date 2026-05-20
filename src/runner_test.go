package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"runAll/src/domain"
	"runAll/src/infrastructure"
)

func TestRunBuild_Success(t *testing.T) {
	runner := &Runner{}
	svc := &Service{
		Name:         "test-build",
		BuildCommand: "echo built",
	}

	ctx := context.Background()
	err := runner.runBuild(ctx, svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunBuild_CommandFailed(t *testing.T) {
	runner := &Runner{}
	svc := &Service{
		Name:         "test-build-fail",
		BuildCommand: "exit 1",
	}

	ctx := context.Background()
	err := runner.runBuild(ctx, svc)
	if err == nil {
		t.Fatal("expected error for failed build command")
	}
	if !strings.Contains(err.Error(), "build failed") {
		t.Errorf("error should mention build failed, got: %v", err)
	}
}

func TestRunBuild_WithWorkingDir(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "built.txt")

	runner := &Runner{}
	svc := &Service{
		Name:         "test-build-dir",
		BuildCommand: "touch " + marker,
		WorkingDir:   dir,
	}

	ctx := context.Background()
	err := runner.runBuild(ctx, svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("build should have created marker file: %v", statErr)
	}
}

func TestRunBuild_WithEnv(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "env-out.txt")

	runner := &Runner{}
	svc := &Service{
		Name:         "test-build-env",
		BuildCommand: "printf '%s' \"$MY_VAR\" > " + marker,
		Env:          map[string]string{"MY_VAR": "hello"},
		WorkingDir:   dir,
	}

	ctx := context.Background()
	err := runner.runBuild(ctx, svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatalf("could not read marker file: %v", readErr)
	}
	if string(data) != "hello" {
		t.Errorf("env output = %q, want %q", string(data), "hello")
	}
}

func TestRunBuild_ContextCanceled(t *testing.T) {
	runner := &Runner{}
	svc := &Service{
		Name:         "test-build-cancel",
		BuildCommand: "sleep 10",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := runner.runBuild(ctx, svc)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestRunBuild_AppendsStructuredLogs(t *testing.T) {
	repo := infrastructure.NewInMemoryServiceLogRepository(100)
	runner := &Runner{logRepository: repo}
	svc := &Service{
		Name:         "test-build-logs",
		BuildCommand: "echo out-line; echo err-line 1>&2",
	}

	ctx := context.Background()
	err := runner.runBuild(ctx, svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var logs []domain.LogEntry
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		logs = repo.Tail("test-build-logs", 10)
		if len(logs) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(logs) < 2 {
		t.Fatalf("expected at least 2 log lines, got %d", len(logs))
	}

	foundOut := false
	foundErr := false
	for _, line := range logs {
		if line.Message == "out-line" && line.Stream == domain.StreamStdout {
			foundOut = true
		}
		if line.Message == "err-line" && line.Stream == domain.StreamStderr {
			foundErr = true
		}
	}
	if !foundOut || !foundErr {
		t.Fatalf("structured logs missing expected entries: %#v", logs)
	}
}

func TestBuildService_Success(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "build-only-ok",
					BuildCommand: "echo built",
					Command:      "echo running",
					HealthCheck:  HealthCheck{URL: "http://localhost:9998"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("build-only-ok", StatusHealthy, "")
	err = runner.BuildService(context.Background(), "build-only-ok")
	if err != nil {
		t.Fatalf("BuildService: unexpected error: %v", err)
	}

	status := store.Get("build-only-ok")
	if status == nil || status.Status != StatusHealthy {
		t.Fatalf("status = %#v, want healthy", status)
	}
}

func TestBuildService_Failure(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "build-only-fail",
					BuildCommand: "exit 7",
					Command:      "echo running",
					HealthCheck:  HealthCheck{URL: "http://localhost:9997"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("build-only-fail", StatusHealthy, "")
	err = runner.BuildService(context.Background(), "build-only-fail")
	if err == nil {
		t.Fatal("expected build failure")
	}
	if !strings.Contains(err.Error(), "build failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	status := store.Get("build-only-fail")
	if status == nil || status.Status != StatusFailed {
		t.Fatalf("status = %#v, want failed", status)
	}
}

func TestBuildService_NotFound(t *testing.T) {
	runner := &Runner{
		cfg:   &Config{},
		store: NewStatusStore(),
	}

	err := runner.BuildService(context.Background(), "missing-service")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildService_StatusConflict(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "build-conflict",
					BuildCommand: "echo built",
					Command:      "echo running",
					HealthCheck:  HealthCheck{URL: "http://localhost:9996"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("build-conflict", StatusRestarting, "")
	err = runner.BuildService(context.Background(), "build-conflict")
	if err == nil {
		t.Fatal("expected status conflict error")
	}
	if !strings.Contains(err.Error(), "can only build healthy or failed services") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildService_ConcurrentBuildRejected(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "build-concurrent",
					BuildCommand: "sleep 1",
					Command:      "echo running",
					HealthCheck:  HealthCheck{URL: "http://localhost:9996"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("build-concurrent", StatusHealthy, "")

	firstErrCh := make(chan error, 1)
	go func() {
		firstErrCh <- runner.BuildService(context.Background(), "build-concurrent")
	}()

	waitForStatus(t, store, "build-concurrent", StatusBuilding, 2*time.Second)

	secondErr := runner.BuildService(context.Background(), "build-concurrent")
	if secondErr == nil {
		t.Fatal("expected second concurrent build to be rejected")
	}
	if !strings.Contains(secondErr.Error(), "can only build healthy or failed services") {
		t.Fatalf("unexpected second error: %v", secondErr)
	}

	firstErr := <-firstErrCh
	if firstErr != nil {
		t.Fatalf("first build should succeed, got: %v", firstErr)
	}
}

func TestRestartService_NoBuildCommand(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "echo-svc",
					Command:     "echo hello",
					HealthCheck: HealthCheck{URL: "http://localhost:9999"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("echo-svc", StatusHealthy, "")

	ctx := context.Background()
	err = runner.RestartService(ctx, "echo-svc")
	// The restart will likely fail at health check (no real server), but it
	// must NOT fail at the build step (there is no BuildCommand).
	if err != nil && strings.Contains(err.Error(), "build") {
		t.Errorf("restart without BuildCommand should not fail with a build error: %v", err)
	}
}

func TestRestartService_WithBuildCommand_Success(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "build-ok",
					BuildCommand: "echo built",
					Command:      "echo running",
					HealthCheck:  HealthCheck{URL: "http://localhost:9998"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("build-ok", StatusHealthy, "")

	ctx := context.Background()
	err = runner.RestartService(ctx, "build-ok")
	// Build succeeds, so error must NOT mention "build failed".
	// The restart will likely fail at health check (no real server), which is expected.
	if err != nil {
		if strings.Contains(err.Error(), "build failed") {
			t.Errorf("build should have succeeded, but got build failure: %v", err)
		}
		// Verify status is NOT StatusFailed from build step — it should have
		// progressed past the build (which succeeded) to the start/health phase.
		status := store.Get("build-ok")
		if status.Status == StatusFailed && strings.Contains(status.Error, "build") {
			t.Errorf("status should not be build-failed after successful build: %+v", status)
		}
	}
}

func TestRestartService_WithBuildCommand_Failure(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "build-fail",
					BuildCommand: "exit 2",
					Command:      "echo running",
					HealthCheck:  HealthCheck{URL: "http://localhost:9997"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("build-fail", StatusHealthy, "")

	ctx := context.Background()
	err = runner.RestartService(ctx, "build-fail")
	if err == nil {
		t.Fatal("expected error for build failure")
	}
	if !strings.Contains(err.Error(), "build failed") {
		t.Errorf("error should mention build failed, got: %v", err)
	}

	status := store.Get("build-fail")
	if status.Status != StatusFailed {
		t.Errorf("status = %q, want %q", status.Status, StatusFailed)
	}
	if status.Error == "" {
		t.Error("error message should be set")
	}
}

func TestRestartService_NotHealthyOrFailed(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "pending-svc",
					Command:     "echo hi",
					HealthCheck: HealthCheck{URL: "http://localhost:9996"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	ctx := context.Background()
	err = runner.RestartService(ctx, "pending-svc")
	if err == nil {
		t.Fatal("expected error for non-healthy/non-failed service")
	}
}

func TestRestartService_NonexistentService(t *testing.T) {
	store := NewStatusStore()
	runner := &Runner{cfg: &Config{}, store: store}

	ctx := context.Background()
	err := runner.RestartService(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestRestartService_DoubleRestartRejected(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "busy-svc",
					Command:     "echo hi",
					HealthCheck: HealthCheck{URL: "http://localhost:9995"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("busy-svc", StatusRestarting, "")

	ctx := context.Background()
	err = runner.RestartService(ctx, "busy-svc")
	if err == nil {
		t.Fatal("expected error when restarting a service that is already restarting")
	}
}

func TestRestartService_DoubleRestartRejected_Building(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "building-svc",
					Command:     "echo hi",
					HealthCheck: HealthCheck{URL: "http://localhost:9994"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("building-svc", StatusBuilding, "")

	ctx := context.Background()
	err = runner.RestartService(ctx, "building-svc")
	if err == nil {
		t.Fatal("expected error when restarting a service that is building")
	}
}

func TestRestartService_BuildFailurePreservesProcess(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "keep-process",
					BuildCommand: "exit 9",
					Command:      "echo running",
					HealthCheck:  HealthCheck{URL: "http://localhost:9993"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("keep-process", StatusHealthy, "")

	// Simulate a running process by putting a dummy cmd in the map
	runner.mu.Lock()
	runner.processes["keep-process"] = exec.Command("echo", "fake-process")
	runner.mu.Unlock()

	ctx := context.Background()
	err = runner.RestartService(ctx, "keep-process")
	if err == nil {
		t.Fatal("expected build failure error")
	}

	// build failed, so stopProcess should NOT have been called.
	// The old process entry must still be in the map.
	runner.mu.Lock()
	_, exists := runner.processes["keep-process"]
	runner.mu.Unlock()
	if !exists {
		t.Error("process should still exist in map after build failure (stopProcess must not be called)")
	}
}

func TestNewRunner_AppliesDefaults(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc",
					Command:     "echo hi",
					HealthCheck: HealthCheck{URL: "http://localhost:9999"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	svc := runner.findService("svc")
	if svc == nil {
		t.Fatal("service not found")
	}
	if svc.HealthCheck.CheckInterval != 10 {
		t.Errorf("CheckInterval default = %d, want 10", svc.HealthCheck.CheckInterval)
	}
	if svc.HealthCheck.UnhealthyThreshold != 2 {
		t.Errorf("UnhealthyThreshold default = %d, want 2", svc.HealthCheck.UnhealthyThreshold)
	}
	if svc.OnFailure != "exit" {
		t.Errorf("OnFailure default = %q, want 'exit'", svc.OnFailure)
	}
}

func waitForStatus(t *testing.T, store *StatusStore, name string, want Status, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s := store.Get(name); s != nil && s.Status == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	got := store.Get(name)
	if got == nil {
		t.Fatalf("timed out waiting for status %q: service %q not found", want, name)
	}
	t.Fatalf("timed out waiting for status %q, got %q", want, got.Status)
}

func TestMonitor_DetectsUnhealthy(t *testing.T) {
	healthy := true
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if healthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	store := NewStatusStore()
	cfg := &Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:    "test-svc",
					Command: "echo hi",
					HealthCheck: HealthCheck{
						URL:                srv.URL,
						Timeout:            30,
						Retries:            10,
						CheckInterval:      1,
						UnhealthyThreshold: 2,
					},
				},
			}},
		},
	}

	runner, err := NewRunner(cfg, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("test-svc", StatusHealthy, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := cfg.Groups[0].Services[0]
	runner.startMonitoring(ctx, svc)

	// Wait for at least one successful check (LastChecked populated)
	waitForLastChecked(t, store, "test-svc", 5*time.Second)

	// Service should still be healthy
	if got := store.Get("test-svc").Status; got != StatusHealthy {
		t.Fatalf("status = %q, want healthy", got)
	}

	// Make service unhealthy
	mu.Lock()
	healthy = false
	mu.Unlock()

	// Wait for monitor to detect unhealthy and mark failed
	waitForStatus(t, store, "test-svc", StatusFailed, 5*time.Second)
}

func waitForLastChecked(t *testing.T, store *StatusStore, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s := store.Get(name); s != nil && s.LastChecked != "" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for LastChecked to be set on %q", name)
}

func TestMonitor_StopsOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewStatusStore()
	cfg := &Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:    "test-svc",
					Command: "echo hi",
					HealthCheck: HealthCheck{
						URL:                srv.URL,
						Timeout:            30,
						Retries:            10,
						CheckInterval:      1,
						UnhealthyThreshold: 2,
					},
				},
			}},
		},
	}

	runner, err := NewRunner(cfg, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("test-svc", StatusHealthy, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := cfg.Groups[0].Services[0]
	runner.startMonitoring(ctx, svc)

	// Wait for at least one check to pass
	waitForLastChecked(t, store, "test-svc", 5*time.Second)

	// Cancel the monitor via stopMonitoring (proper path)
	runner.stopMonitoring("test-svc")

	runner.monitorMu.Lock()
	_, exists := runner.monitors["test-svc"]
	runner.monitorMu.Unlock()
	if exists {
		t.Error("monitor entry should be removed after stopMonitoring")
	}
}

func TestMonitor_CleansUpAfterUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	store := NewStatusStore()
	cfg := &Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:    "test-svc",
					Command: "echo hi",
					HealthCheck: HealthCheck{
						URL:                srv.URL,
						Timeout:            30,
						Retries:            10,
						CheckInterval:      1,
						UnhealthyThreshold: 1,
					},
				},
			}},
		},
	}

	runner, err := NewRunner(cfg, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("test-svc", StatusHealthy, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := cfg.Groups[0].Services[0]
	runner.startMonitoring(ctx, svc)

	// Wait for monitor to detect unhealthy and exit
	waitForStatus(t, store, "test-svc", StatusFailed, 5*time.Second)

	// Monitor entry should be cleaned up after goroutine exits
	runner.monitorMu.Lock()
	_, exists := runner.monitors["test-svc"]
	runner.monitorMu.Unlock()
	if exists {
		t.Error("monitor entry should be removed after goroutine exits via unhealthy threshold")
	}
}

func TestStartAndCheck_ProcessSurvivesContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewStatusStore()
	store.Init([]string{"sleep-svc"})

	cfg := &Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:    "sleep-svc",
					Command: "sleep 30",
					HealthCheck: HealthCheck{
						URL:     srv.URL,
						Timeout: 5,
						Retries: 3,
						Backoff: Backoff{Initial: 0.1, Max: 0.5, Multiplier: 2.0},
					},
					OnFailure: "exit",
				},
			}},
		},
	}

	runner := &Runner{
		cfg:       cfg,
		store:     store,
		processes: make(map[string]*exec.Cmd),
		monitors:  make(map[string]context.CancelFunc),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := &ServiceNode{Service: cfg.Flatten()[0]}

	// Start the service and wait for healthy
	err := runner.startAndCheck(ctx, node)
	if err != nil {
		t.Fatalf("startAndCheck: unexpected error: %v", err)
	}

	status := store.Get("sleep-svc")
	if status.Status != StatusHealthy {
		t.Fatalf("status = %q, want healthy", status.Status)
	}

	// Cancel the context — simulating DAG level completion
	cancel()

	// Give the kill signal time to arrive if it were still using CommandContext
	time.Sleep(200 * time.Millisecond)

	// The process MUST still be alive
	runner.mu.Lock()
	cmd := runner.processes["sleep-svc"]
	runner.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		t.Fatal("process should still be tracked after context cancel")
	}

	// Signal 0 checks if the process exists without sending a real signal
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("process should survive context cancel, but got: %v", err)
	}

	// Cleanup
	runner.stopProcess("sleep-svc")
}
