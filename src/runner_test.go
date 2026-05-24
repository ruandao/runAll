package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestBuildService_SuccessWhenStopped(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "build-only-stopped",
					BuildCommand: "echo built",
					Command:      "echo running",
					HealthCheck:  HealthCheck{URL: "http://localhost:9994"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("build-only-stopped", StatusStopped, "")
	err = runner.BuildService(context.Background(), "build-only-stopped")
	if err != nil {
		t.Fatalf("BuildService: unexpected error: %v", err)
	}

	status := store.Get("build-only-stopped")
	if status == nil || status.Status != StatusStopped {
		t.Fatalf("status = %#v, want stopped", status)
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
	if !strings.Contains(err.Error(), "can only build healthy, failed, or stopped services") {
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
	if !strings.Contains(secondErr.Error(), "can only build healthy, failed, or stopped services") {
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

func TestRestartService_WithBuildCommand_FailureKeepsPreviousStatus(t *testing.T) {
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
	if status.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", status.Status, StatusHealthy)
	}
	if status.Error == "" {
		t.Error("error message should be set")
	}
}

func TestRestartService_StoppedServiceAllowed(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "stopped-svc",
					Command:     "echo hi",
					HealthCheck: HealthCheck{URL: "http://localhost:9993"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("stopped-svc", StatusStopped, "")

	ctx := context.Background()
	err = runner.RestartService(ctx, "stopped-svc")
	if err != nil && strings.Contains(err.Error(), "can only restart healthy, failed, stopped, or pending services") {
		t.Fatalf("stopped service should be restartable, got: %v", err)
	}
}

func TestRestartService_PendingServiceAllowed(t *testing.T) {
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
	store.Update("pending-svc", StatusPending, "")

	ctx := context.Background()
	err = runner.RestartService(ctx, "pending-svc")
	if err != nil && strings.Contains(err.Error(), "can only restart healthy, failed, stopped, or pending services") {
		t.Fatalf("pending service should be restartable, got: %v", err)
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

	status := store.Get("keep-process")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.Status != StatusHealthy {
		t.Fatalf("status = %s, want %s when build fails before stop", status.Status, StatusHealthy)
	}
}

func TestRunner_StopService_BlockedByActiveDownstreamDependency(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "db",
						Command:     "echo db",
						HealthCheck: HealthCheck{URL: "http://localhost:9101"},
					},
					{
						Name:        "api",
						Command:     "echo api",
						DependsOn:   []string{"db"},
						HealthCheck: HealthCheck{URL: "http://localhost:9102"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("db", StatusHealthy, "")
	store.Update("api", StatusHealthy, "")

	err = runner.StopService(context.Background(), "db")
	if err == nil {
		t.Fatal("expected stop to be blocked by active downstream dependency")
	}
	if !strings.Contains(err.Error(), "active downstream dependencies") || !strings.Contains(err.Error(), "api") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_StopService_SetsStoppedStatus(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "worker",
						Command:     "echo worker",
						HealthCheck: HealthCheck{URL: "http://localhost:9201"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("worker", StatusHealthy, "")

	if err := runner.StopService(context.Background(), "worker"); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	status := store.Get("worker")
	if status == nil || status.Status != StatusStopped {
		t.Fatalf("status = %#v, want stopped", status)
	}
}

func TestRunner_StopService_AllowsRetryingStatus(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "retrying-worker",
						Command:     "echo worker",
						HealthCheck: HealthCheck{URL: "http://localhost:9202"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("retrying-worker", StatusRetrying, "")

	if err := runner.StopService(context.Background(), "retrying-worker"); err != nil {
		t.Fatalf("StopService should allow retrying status, got: %v", err)
	}

	status := store.Get("retrying-worker")
	if status == nil || status.Status != StatusStopped {
		t.Fatalf("status = %#v, want stopped", status)
	}
}

func TestRunner_StopService_ClearsPID(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "pid-worker",
						Command:     "echo worker",
						HealthCheck: HealthCheck{URL: "http://localhost:9203"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("pid-worker", StatusHealthy, "")
	store.SetPID("pid-worker", 12345)

	if err := runner.StopService(context.Background(), "pid-worker"); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	status := store.Get("pid-worker")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.PID != 0 {
		t.Fatalf("pid = %d, want 0", status.PID)
	}
}

func TestRunner_StopService_StopsDetachedListenerByPortFallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/", port)

	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:    "detached-listener",
						Command: "echo managed-externally",
						HealthCheck: HealthCheck{
							URL:     healthURL,
							Timeout: 5,
							Retries: 10,
							Backoff: Backoff{
								Initial:    0.1,
								Max:        0.2,
								Multiplier: 1.2,
							},
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	pid := startDetachedHTTPServer(t, port)
	defer func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}()
	waitForPortOpen(t, port, 5*time.Second)

	store.Update("detached-listener", StatusHealthy, "")
	store.SetPID("detached-listener", 0)

	if _, exists := runner.processes["detached-listener"]; exists {
		t.Fatal("test setup expects no tracked process entry")
	}

	if err := runner.StopService(context.Background(), "detached-listener"); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	waitForPortClosed(t, port, 5*time.Second)
	status := store.Get("detached-listener")
	if status == nil || status.Status != StatusStopped {
		t.Fatalf("status = %#v, want stopped", status)
	}
}

func TestRunner_StartService_FromStopped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:    "startable",
						Command: "sleep 30",
						HealthCheck: HealthCheck{
							URL:           srv.URL,
							Timeout:       2,
							Retries:       2,
							CheckInterval: 1,
							Backoff:       Backoff{Initial: 0.1, Max: 0.2, Multiplier: 1.5},
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("startable", StatusStopped, "")

	if err := runner.StartService(context.Background(), "startable"); err != nil {
		t.Fatalf("StartService: %v", err)
	}

	got := store.Get("startable")
	if got == nil || got.Status != StatusHealthy {
		t.Fatalf("status = %#v, want healthy", got)
	}

	runner.monitorMu.Lock()
	_, monitorExists := runner.monitors["startable"]
	runner.monitorMu.Unlock()
	if !monitorExists {
		t.Fatal("expected monitor to be resumed after start")
	}

	if err := runner.StopService(context.Background(), "startable"); err != nil {
		t.Fatalf("cleanup StopService: %v", err)
	}
}

func TestRunner_StartService_RejectsNonStoppedStatus(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "busy-svc",
						Command:     "echo busy",
						HealthCheck: HealthCheck{URL: "http://localhost:9302"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("busy-svc", StatusHealthy, "")

	err = runner.StartService(context.Background(), "busy-svc")
	if err == nil {
		t.Fatal("expected start to be rejected for non-stopped status")
	}
	if !strings.Contains(err.Error(), "can only start stopped") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_StartService_OnFailureSkipStillReturnsError(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:    "skip-start",
						Command: "sleep 1",
						HealthCheck: HealthCheck{
							URL:     "http://127.0.0.1:65534/unhealthy",
							Timeout: 1,
							Retries: 1,
							Backoff: Backoff{
								Initial:    0.1,
								Max:        0.1,
								Multiplier: 1.0,
							},
						},
						OnFailure: "skip",
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("skip-start", StatusStopped, "")
	err = runner.StartService(context.Background(), "skip-start")
	if err == nil {
		t.Fatal("expected start failure error even when on_failure=skip")
	}
}

func TestRunner_StartService_FailureUpdatesDependentStatus(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:    "worker",
						Command: "sleep 1",
						HealthCheck: HealthCheck{
							URL:     "http://127.0.0.1:65534/unhealthy",
							Timeout: 1,
							Retries: 1,
							Backoff: Backoff{
								Initial:    0.1,
								Max:        0.1,
								Multiplier: 1.0,
							},
						},
						OnFailure: "skip",
					},
					{
						Name:      "api",
						Command:   "echo api",
						DependsOn: []string{"worker"},
						HealthCheck: HealthCheck{
							URL: "http://localhost:9401",
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("worker", StatusStopped, "")
	if err := runner.StartService(context.Background(), "worker"); err == nil {
		t.Fatal("expected start to fail")
	}

	apiStatus := store.Get("api")
	if apiStatus == nil {
		t.Fatal("api status should exist")
	}
	if len(apiStatus.DependsOn) != 1 {
		t.Fatalf("depends_on length = %d, want 1", len(apiStatus.DependsOn))
	}
	if apiStatus.DependsOn[0].Status != StatusFailed {
		t.Fatalf("dependency status = %s, want %s", apiStatus.DependsOn[0].Status, StatusFailed)
	}
}

func TestRunner_StartService_OnFailureExitCleansUpProcessAndPID(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:    "exit-start",
						Command: "sleep 1",
						HealthCheck: HealthCheck{
							URL:     "http://127.0.0.1:65534/unhealthy",
							Timeout: 1,
							Retries: 1,
							Backoff: Backoff{
								Initial:    0.1,
								Max:        0.1,
								Multiplier: 1.0,
							},
						},
						OnFailure: "exit",
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("exit-start", StatusStopped, "")
	err = runner.StartService(context.Background(), "exit-start")
	if err == nil {
		t.Fatal("expected start to fail")
	}

	status := store.Get("exit-start")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.PID != 0 {
		t.Fatalf("pid = %d, want 0", status.PID)
	}

	runner.mu.Lock()
	_, exists := runner.processes["exit-start"]
	runner.mu.Unlock()
	if exists {
		t.Fatal("process should be removed after failed start cleanup")
	}
}

func TestRunner_RestartService_OnFailureSkipReturnsErrorAndCleansUp(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:    "restart-skip",
						Command: "sleep 1",
						HealthCheck: HealthCheck{
							URL:     "http://127.0.0.1:65534/unhealthy",
							Timeout: 1,
							Retries: 1,
							Backoff: Backoff{
								Initial:    0.1,
								Max:        0.1,
								Multiplier: 1.0,
							},
						},
						OnFailure: "skip",
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("restart-skip", StatusHealthy, "")
	err = runner.RestartService(context.Background(), "restart-skip")
	if err == nil {
		t.Fatal("expected restart failure error when health check fails")
	}

	status := store.Get("restart-skip")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.PID != 0 {
		t.Fatalf("pid = %d, want 0", status.PID)
	}
}

func TestRunner_StopService_BlocksFailedDependentWithRunningPID(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "db",
						Command:     "echo db",
						HealthCheck: HealthCheck{URL: "http://localhost:9501"},
					},
					{
						Name:      "api",
						Command:   "echo api",
						DependsOn: []string{"db"},
						HealthCheck: HealthCheck{
							URL: "http://localhost:9502",
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.Update("db", StatusHealthy, "")
	store.Update("api", StatusFailed, "health failed")
	store.SetPID("api", 9876)

	err = runner.StopService(context.Background(), "db")
	if err == nil {
		t.Fatal("expected stop to be blocked by failed dependent with running pid")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_StopGroup_GroupNotFound(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "existing",
				Services: []Service{
					{
						Name:        "svc",
						Command:     "echo svc",
						HealthCheck: HealthCheck{URL: "http://localhost:9301"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	err = runner.StopGroup(context.Background(), "missing-group")
	if err == nil {
		t.Fatal("expected group not found error")
	}
	if !strings.Contains(err.Error(), "group \"missing-group\" not found") {
		t.Fatalf("unexpected error: %v", err)
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

func waitForPortClosed(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 150*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("port %d still open after %s", port, timeout)
}

func waitForPortOpen(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 150*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("port %d did not open within %s", port, timeout)
}

func startDetachedHTTPServer(t *testing.T, port int) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", fmt.Sprintf("nohup python3 -m http.server %d >/dev/null 2>&1 & echo $!", port))
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("start detached server: %v", err)
	}
	pidText := strings.TrimSpace(string(output))
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatalf("parse detached pid %q: %v", pidText, err)
	}
	return pid
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
