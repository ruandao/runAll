package main

import (
	"context"
	"errors"
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

type runtimePrereqProbeRepositoryStub struct {
	err error
}

func (s runtimePrereqProbeRepositoryStub) Probe(service domain.ManagedService) error {
	_ = service
	return s.err
}

type deleteOwnershipFailingRepositoryStub struct {
	delegate  domain.ServiceOwnershipRepository
	deleteErr error
}

func (s deleteOwnershipFailingRepositoryStub) FindByServiceName(serviceName string) (domain.ServiceOwnership, error) {
	return s.delegate.FindByServiceName(serviceName)
}

func (s deleteOwnershipFailingRepositoryStub) Save(ownership domain.ServiceOwnership) error {
	return s.delegate.Save(ownership)
}

func (s deleteOwnershipFailingRepositoryStub) DeleteByServiceName(serviceName string) error {
	_ = serviceName
	return s.deleteErr
}

func (s deleteOwnershipFailingRepositoryStub) ListAll() ([]domain.ServiceOwnership, error) {
	return s.delegate.ListAll()
}

func stubNoPortListenersForTest(runner *Runner) {
	if runner == nil {
		return
	}
	runner.listenerPIDsFn = func(string) ([]int, error) {
		return nil, nil
	}
}

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

func TestStopService_RejectsNonOwnerSession(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "owned-stop",
						Command:     "echo worker",
						HealthCheck: HealthCheck{URL: "http://localhost:9210"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("owned-stop", StatusHealthy, "")

	ownership, err := domain.NewServiceOwnership(
		"owned-stop",
		"owner-session",
		1234,
		"config-hash",
		"http://localhost:9210",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := runner.ownershipRepo.Save(ownership); err != nil {
		t.Fatalf("Save ownership: %v", err)
	}

	err = runner.StopServiceWithActor(context.Background(), "owned-stop", "other-session")
	if err == nil {
		t.Fatal("expected non-owner stop to be rejected")
	}
	if !strings.Contains(err.Error(), "requires explicit takeover") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStopServiceWithActor_ClearsOwnershipAfterStopped(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "owned-stop-clear",
						Command:     "echo worker",
						HealthCheck: HealthCheck{URL: "http://localhost:9213"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("owned-stop-clear", StatusHealthy, "")
	store.SetPID("owned-stop-clear", 2233)

	ownership, err := domain.NewServiceOwnership(
		"owned-stop-clear",
		"owner-session",
		2233,
		"config-hash",
		"http://localhost:9213",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := runner.ownershipRepo.Save(ownership); err != nil {
		t.Fatalf("Save ownership: %v", err)
	}

	if err := runner.StopServiceWithActor(context.Background(), "owned-stop-clear", "owner-session"); err != nil {
		t.Fatalf("StopServiceWithActor: %v", err)
	}

	cleared, err := runner.ownershipRepo.FindByServiceName("owned-stop-clear")
	if err != nil {
		t.Fatalf("FindByServiceName: %v", err)
	}
	if cleared.ServiceName != "" || cleared.OwnerSessionID != "" || cleared.PID != 0 {
		t.Fatalf("ownership should be cleared after stop, got: %#v", cleared)
	}
}

func TestStopServiceWithActor_OwnershipCleanupFailureReturnsError(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "owned-stop-cleanup-fail",
						Command:     "echo worker",
						HealthCheck: HealthCheck{URL: "http://localhost:9214"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("owned-stop-cleanup-fail", StatusHealthy, "")

	ownership, err := domain.NewServiceOwnership(
		"owned-stop-cleanup-fail",
		"owner-session",
		2233,
		"config-hash",
		"http://localhost:9214",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := runner.ownershipRepo.Save(ownership); err != nil {
		t.Fatalf("Save ownership: %v", err)
	}

	runner.ownershipRepo = deleteOwnershipFailingRepositoryStub{
		delegate:  runner.ownershipRepo,
		deleteErr: errors.New("delete ownership boom"),
	}

	err = runner.StopServiceWithActor(context.Background(), "owned-stop-cleanup-fail", "owner-session")
	if err == nil {
		t.Fatal("expected stop to fail when ownership cleanup fails")
	}
	if !strings.Contains(err.Error(), "ownership cleanup failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	status := store.Get("owned-stop-cleanup-fail")
	if status == nil {
		t.Fatal("expected service status")
	}
	if status.Status != StatusFailed {
		t.Fatalf("status = %s, want %s", status.Status, StatusFailed)
	}
	if !strings.Contains(status.Error, "ownership cleanup failed") {
		t.Fatalf("status error = %q, want ownership cleanup failed", status.Error)
	}
}

func TestStopCleanupFailure_TakeoverThenStartServiceWithActor_Recovers(t *testing.T) {
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
						Name:    "cleanup-fail-recover",
						Command: "sleep 30",
						HealthCheck: HealthCheck{
							URL:     srv.URL,
							Timeout: 2,
							Retries: 2,
							Backoff: Backoff{Initial: 0.1, Max: 0.2, Multiplier: 1.5},
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("cleanup-fail-recover", StatusStopped, "")
	runner.listenerPIDsFn = func(string) ([]int, error) {
		return nil, nil
	}

	if err := runner.StartServiceWithActor(context.Background(), "cleanup-fail-recover", "owner-session"); err != nil {
		t.Fatalf("StartServiceWithActor(owner): %v", err)
	}

	originalOwnershipRepo := runner.ownershipRepo
	runner.ownershipRepo = deleteOwnershipFailingRepositoryStub{
		delegate:  originalOwnershipRepo,
		deleteErr: errors.New("delete ownership boom"),
	}

	err = runner.StopServiceWithActor(context.Background(), "cleanup-fail-recover", "owner-session")
	if err == nil {
		t.Fatal("expected stop to fail when ownership cleanup fails")
	}

	statusAfterStop := store.Get("cleanup-fail-recover")
	if statusAfterStop == nil || statusAfterStop.Status != StatusFailed {
		t.Fatalf("status after stop = %#v, want failed", statusAfterStop)
	}

	if err := runner.TakeoverService("cleanup-fail-recover", "other-session"); err != nil {
		t.Fatalf("TakeoverService(other): %v", err)
	}

	if err := runner.StartServiceWithActor(context.Background(), "cleanup-fail-recover", "other-session"); err != nil {
		t.Fatalf("StartServiceWithActor(other): %v", err)
	}

	statusAfterRecover := store.Get("cleanup-fail-recover")
	if statusAfterRecover == nil || statusAfterRecover.Status != StatusHealthy {
		t.Fatalf("status after recover = %#v, want healthy", statusAfterRecover)
	}

	runner.ownershipRepo = originalOwnershipRepo
	runner.ownershipGuard = domain.NewServiceOwnershipGuardService(originalOwnershipRepo)
	if err := runner.StopServiceWithActor(context.Background(), "cleanup-fail-recover", "other-session"); err != nil {
		t.Fatalf("cleanup StopServiceWithActor(other): %v", err)
	}
}

func TestRestartService_RejectsNonOwnerSession(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "owned-restart",
						Command:     "echo worker",
						HealthCheck: HealthCheck{URL: "http://localhost:9211"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("owned-restart", StatusHealthy, "")

	ownership, err := domain.NewServiceOwnership(
		"owned-restart",
		"owner-session",
		1234,
		"config-hash",
		"http://localhost:9211",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := runner.ownershipRepo.Save(ownership); err != nil {
		t.Fatalf("Save ownership: %v", err)
	}

	err = runner.RestartServiceWithActor(context.Background(), "owned-restart", "other-session")
	if err == nil {
		t.Fatal("expected non-owner restart to be rejected")
	}
	if !strings.Contains(err.Error(), "requires explicit takeover") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTakeoverService_DiscoversListenerPIDWhenStorePIDMissing(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "owned-takeover",
						Command:     "echo worker --port 9212",
						HealthCheck: HealthCheck{URL: "http://localhost:9212/health"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("owned-takeover", StatusHealthy, "")
	store.SetPID("owned-takeover", 0)

	runner.listenerPIDsFn = func(port string) ([]int, error) {
		if port == "9212" {
			return []int{4321}, nil
		}
		return nil, nil
	}

	if err := runner.TakeoverService("owned-takeover", "takeover-session"); err != nil {
		t.Fatalf("TakeoverService: %v", err)
	}

	ownership, err := runner.ownershipRepo.FindByServiceName("owned-takeover")
	if err != nil {
		t.Fatalf("FindByServiceName: %v", err)
	}
	if ownership.OwnerSessionID != "takeover-session" {
		t.Fatalf("owner session = %q, want takeover-session", ownership.OwnerSessionID)
	}
	if ownership.PID != 4321 {
		t.Fatalf("ownership pid = %d, want 4321", ownership.PID)
	}
}

func TestTakeoverService_StoppedServiceWithResidualOwnershipAllowsExplicitTakeover(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "owned-stopped-takeover",
						Command:     "echo worker",
						HealthCheck: HealthCheck{URL: "http://localhost:9215"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("owned-stopped-takeover", StatusStopped, "")
	store.SetPID("owned-stopped-takeover", 0)

	ownership, err := domain.NewServiceOwnership(
		"owned-stopped-takeover",
		"owner-session",
		2233,
		"config-hash",
		"http://localhost:9215",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := runner.ownershipRepo.Save(ownership); err != nil {
		t.Fatalf("Save ownership: %v", err)
	}

	if err := runner.TakeoverService("owned-stopped-takeover", "other-session"); err != nil {
		t.Fatalf("TakeoverService: %v", err)
	}

	takenOwnership, err := runner.ownershipRepo.FindByServiceName("owned-stopped-takeover")
	if err != nil {
		t.Fatalf("FindByServiceName: %v", err)
	}
	if takenOwnership.OwnerSessionID != "other-session" {
		t.Fatalf("owner session = %q, want other-session", takenOwnership.OwnerSessionID)
	}
}

func TestTakeoverService_ThenStartServiceWithActor_AllowsRecoveryAfterStoppedResidualOwnership(t *testing.T) {
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
						Name:    "stopped-recovery",
						Command: "sleep 30",
						HealthCheck: HealthCheck{
							URL:     srv.URL,
							Timeout: 2,
							Retries: 2,
							Backoff: Backoff{Initial: 0.1, Max: 0.2, Multiplier: 1.5},
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("stopped-recovery", StatusStopped, "")
	store.SetPID("stopped-recovery", 0)
	runner.listenerPIDsFn = func(string) ([]int, error) {
		return nil, nil
	}

	ownership, err := domain.NewServiceOwnership(
		"stopped-recovery",
		"owner-session",
		2233,
		"config-hash",
		srv.URL,
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := runner.ownershipRepo.Save(ownership); err != nil {
		t.Fatalf("Save ownership: %v", err)
	}

	if err := runner.TakeoverService("stopped-recovery", "other-session"); err != nil {
		t.Fatalf("TakeoverService: %v", err)
	}

	if err := runner.StartServiceWithActor(context.Background(), "stopped-recovery", "other-session"); err != nil {
		t.Fatalf("StartServiceWithActor: %v", err)
	}

	status := store.Get("stopped-recovery")
	if status == nil || status.Status != StatusHealthy {
		t.Fatalf("status = %#v, want healthy", status)
	}

	takenOwnership, err := runner.ownershipRepo.FindByServiceName("stopped-recovery")
	if err != nil {
		t.Fatalf("FindByServiceName: %v", err)
	}
	if takenOwnership.OwnerSessionID != "other-session" {
		t.Fatalf("owner session = %q, want other-session", takenOwnership.OwnerSessionID)
	}

	if err := runner.StopServiceWithActor(context.Background(), "stopped-recovery", "other-session"); err != nil {
		t.Fatalf("cleanup StopServiceWithActor: %v", err)
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

func TestRun_CleansForeignPortConflictBeforeStart(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/", port)
	command := fmt.Sprintf("python3 -m http.server %d", port)
	foreignPID := startDetachedHTTPServer(t, port)
	defer func() {
		_ = syscall.Kill(foreignPID, syscall.SIGKILL)
	}()
	waitForPortOpen(t, port, 5*time.Second)

	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "conflict-svc",
						Command:     command,
						HealthCheck: HealthCheck{URL: healthURL},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	err = runner.Run(context.Background(), true)
	if err != nil {
		t.Fatalf("expected run to succeed after port cleanup, got: %v", err)
	}
	defer runner.Shutdown()

	waitForProcessExit(t, foreignPID, 5*time.Second)

	status := store.Get("conflict-svc")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.Status != StatusHealthy {
		t.Fatalf("status = %s, want %s", status.Status, StatusHealthy)
	}
	if status.PID <= 0 {
		t.Fatalf("status pid = %d, want positive", status.PID)
	}
	if status.PID == foreignPID {
		t.Fatalf("service should not reuse foreign pid %d", foreignPID)
	}
}

func TestRun_PreflightGenericErrorMarksServiceFailed(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "preflight-generic-fail",
						Command:     "sleep 30",
						HealthCheck: HealthCheck{URL: "http://127.0.0.1:65535/"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	runner.preflightFn = func(context.Context, Service) error {
		return errors.New("lsof probe failed")
	}

	err = runner.Run(context.Background(), true)
	if err == nil {
		t.Fatal("expected run to fail on generic preflight error")
	}
	if !strings.Contains(err.Error(), "lsof probe failed") {
		t.Fatalf("unexpected run error: %v", err)
	}

	status := store.Get("preflight-generic-fail")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.Status != StatusFailed {
		t.Fatalf("status = %s, want %s", status.Status, StatusFailed)
	}
	if !strings.Contains(status.Error, "preflight failed") || !strings.Contains(status.Error, "lsof probe failed") {
		t.Fatalf("status error should contain diagnostic preflight failure, got: %q", status.Error)
	}
}

func TestRun_BlocksOnRuntimePrereqFailure(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "runtime-prereq-fail",
						Command:     "sleep 30",
						HealthCheck: HealthCheck{URL: "http://127.0.0.1:65535/"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	runner.runtimePrereqProbeRepository = runtimePrereqProbeRepositoryStub{
		err: errors.New("docker daemon unavailable"),
	}

	err = runner.Run(context.Background(), true)
	if err == nil {
		t.Fatal("expected run to fail on runtime prerequisite probe error")
	}
	if !strings.Contains(err.Error(), domain.ServiceFailureCodeRuntimePrereq) {
		t.Fatalf("error should contain %s, got: %v", domain.ServiceFailureCodeRuntimePrereq, err)
	}

	status := store.Get("runtime-prereq-fail")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.Status != StatusFailed {
		t.Fatalf("status = %s, want %s", status.Status, StatusFailed)
	}
	if status.FailureCode != domain.ServiceFailureCodeRuntimePrereq {
		t.Fatalf("failure code = %q, want %q", status.FailureCode, domain.ServiceFailureCodeRuntimePrereq)
	}
}

func TestRun_SuccessEstablishesOwnership(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/", port)
	command := fmt.Sprintf("python3 -m http.server %d", port)

	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:    "run-owned",
						Command: command,
						HealthCheck: HealthCheck{
							URL:     healthURL,
							Timeout: 2,
							Retries: 2,
							Backoff: Backoff{Initial: 0.1, Max: 0.2, Multiplier: 1.5},
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer runner.Shutdown()

	if err := runner.Run(context.Background(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}

	ownership, err := runner.ownershipRepo.FindByServiceName("run-owned")
	if err != nil {
		t.Fatalf("FindByServiceName: %v", err)
	}
	if ownership.OwnerSessionID != defaultOwnershipSessionID {
		t.Fatalf("owner session = %q, want %q", ownership.OwnerSessionID, defaultOwnershipSessionID)
	}
	if ownership.PID <= 0 {
		t.Fatalf("ownership pid = %d, want positive", ownership.PID)
	}
}

func TestRun_RegressionMatrix(t *testing.T) {
	t.Run("port conflict cleaned", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}

		healthURL := fmt.Sprintf("http://127.0.0.1:%d/", port)
		command := fmt.Sprintf("python3 -m http.server %d", port)
		foreignPID := startDetachedHTTPServer(t, port)
		defer func() {
			_ = syscall.Kill(foreignPID, syscall.SIGKILL)
		}()
		waitForPortOpen(t, port, 5*time.Second)

		store := NewStatusStore()
		runner, err := NewRunner(&Config{
			Version: "1",
			Groups: []Group{
				{
					Name: "matrix",
					Services: []Service{
						{
							Name:        "matrix-port-conflict",
							Command:     command,
							HealthCheck: HealthCheck{URL: healthURL},
						},
					},
				},
			},
		}, store)
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}

		err = runner.Run(context.Background(), true)
		if err != nil {
			t.Fatalf("expected run to succeed after port cleanup, got: %v", err)
		}
		defer runner.Shutdown()
		waitForProcessExit(t, foreignPID, 5*time.Second)

		status := store.Get("matrix-port-conflict")
		if status == nil {
			t.Fatal("status should exist")
		}
		if status.Status != StatusHealthy {
			t.Fatalf("status = %s, want %s", status.Status, StatusHealthy)
		}
	})

	t.Run("runtime prereq blocked", func(t *testing.T) {
		store := NewStatusStore()
		runner, err := NewRunner(&Config{
			Version: "1",
			Groups: []Group{
				{
					Name: "matrix",
					Services: []Service{
						{
							Name:        "matrix-runtime-blocked",
							Command:     "sleep 30",
							HealthCheck: HealthCheck{URL: "http://127.0.0.1:65535/"},
						},
					},
				},
			},
		}, store)
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}

		runner.runtimePrereqProbeRepository = runtimePrereqProbeRepositoryStub{
			err: errors.New("docker daemon unavailable"),
		}

		err = runner.Run(context.Background(), true)
		if err == nil {
			t.Fatal("expected run to fail on runtime prerequisite probe error")
		}
		if !strings.Contains(err.Error(), domain.ServiceFailureCodeRuntimePrereq) {
			t.Fatalf("error should contain %s, got: %v", domain.ServiceFailureCodeRuntimePrereq, err)
		}

		status := store.Get("matrix-runtime-blocked")
		if status == nil {
			t.Fatal("status should exist")
		}
		if status.FailureCode != domain.ServiceFailureCodeRuntimePrereq {
			t.Fatalf("failure code = %q, want %q", status.FailureCode, domain.ServiceFailureCodeRuntimePrereq)
		}
	})

	t.Run("prereq repaired healthy", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}
		healthURL := fmt.Sprintf("http://127.0.0.1:%d/", port)
		command := fmt.Sprintf("python3 -m http.server %d", port)

		store := NewStatusStore()
		runner, err := NewRunner(&Config{
			Version: "1",
			Groups: []Group{
				{
					Name: "matrix",
					Services: []Service{
						{
							Name:        "matrix-prereq-repaired",
							Command:     command,
							HealthCheck: HealthCheck{URL: healthURL},
						},
					},
				},
			},
		}, store)
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}
		defer runner.Shutdown()

		runner.runtimePrereqProbeRepository = runtimePrereqProbeRepositoryStub{
			err: errors.New("docker daemon unavailable"),
		}
		err = runner.Run(context.Background(), true)
		if err == nil {
			t.Fatal("expected first run to fail while runtime prereq is blocked")
		}

		runner.runtimePrereqProbeRepository = runtimePrereqProbeRepositoryStub{}
		err = runner.Run(context.Background(), true)
		if err != nil {
			t.Fatalf("expected second run to succeed after prereq repaired, got: %v", err)
		}

		status := store.Get("matrix-prereq-repaired")
		if status == nil {
			t.Fatal("status should exist")
		}
		if status.Status != StatusHealthy {
			t.Fatalf("status = %s, want %s", status.Status, StatusHealthy)
		}
		if status.FailureCode != "" {
			t.Fatalf("failure code = %q, want empty after successful rerun", status.FailureCode)
		}
	})

	t.Run("non-owner rejected", func(t *testing.T) {
		store := NewStatusStore()
		runner, err := NewRunner(&Config{
			Version: "1",
			Groups: []Group{
				{
					Name: "matrix",
					Services: []Service{
						{
							Name:        "matrix-owned",
							Command:     "echo worker",
							HealthCheck: HealthCheck{URL: "http://localhost:9311"},
						},
					},
				},
			},
		}, store)
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}
		store.Update("matrix-owned", StatusHealthy, "")

		ownership, err := domain.NewServiceOwnership(
			"matrix-owned",
			"owner-session",
			2233,
			"config-hash",
			"http://localhost:9311",
			time.Now(),
		)
		if err != nil {
			t.Fatalf("NewServiceOwnership: %v", err)
		}
		if err := runner.ownershipRepo.Save(ownership); err != nil {
			t.Fatalf("Save ownership: %v", err)
		}

		err = runner.StopServiceWithActor(context.Background(), "matrix-owned", "other-session")
		if err == nil {
			t.Fatal("expected non-owner stop to be rejected")
		}
		if !strings.Contains(err.Error(), "requires explicit takeover") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
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
	stubNoPortListenersForTest(runner)
	store.Update("startable", StatusStopped, "")

	if err := runner.StartService(context.Background(), "startable"); err != nil {
		t.Fatalf("StartService: %v", err)
	}

	got := store.Get("startable")
	if got == nil || got.Status != StatusHealthy {
		t.Fatalf("status = %#v, want healthy", got)
	}
	ownership, err := runner.ownershipRepo.FindByServiceName("startable")
	if err != nil {
		t.Fatalf("FindByServiceName: %v", err)
	}
	if ownership.OwnerSessionID != defaultOwnershipSessionID {
		t.Fatalf("owner session = %q, want %q", ownership.OwnerSessionID, defaultOwnershipSessionID)
	}
	if ownership.PID <= 0 {
		t.Fatalf("ownership pid = %d, want positive", ownership.PID)
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

func TestRunner_StartServiceWithActor_EstablishesOwnership(t *testing.T) {
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
						Name:    "start-with-actor",
						Command: "sleep 30",
						HealthCheck: HealthCheck{
							URL:     srv.URL,
							Timeout: 2,
							Retries: 2,
							Backoff: Backoff{Initial: 0.1, Max: 0.2, Multiplier: 1.5},
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	stubNoPortListenersForTest(runner)
	store.Update("start-with-actor", StatusStopped, "")

	if err := runner.StartServiceWithActor(context.Background(), "start-with-actor", "ui-session-1"); err != nil {
		t.Fatalf("StartServiceWithActor: %v", err)
	}

	ownership, err := runner.ownershipRepo.FindByServiceName("start-with-actor")
	if err != nil {
		t.Fatalf("FindByServiceName: %v", err)
	}
	if ownership.OwnerSessionID != "ui-session-1" {
		t.Fatalf("owner session = %q, want ui-session-1", ownership.OwnerSessionID)
	}
	if ownership.PID <= 0 {
		t.Fatalf("ownership pid = %d, want positive", ownership.PID)
	}

	if err := runner.StopServiceWithActor(context.Background(), "start-with-actor", "ui-session-1"); err != nil {
		t.Fatalf("cleanup StopServiceWithActor: %v", err)
	}
}

func TestRunner_StartServiceWithActor_RejectsNonOwner(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{
						Name:        "owned-start",
						Command:     "echo worker",
						HealthCheck: HealthCheck{URL: "http://localhost:9511"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("owned-start", StatusStopped, "")

	ownership, err := domain.NewServiceOwnership(
		"owned-start",
		"owner-session",
		1234,
		"config-hash",
		"http://localhost:9511",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := runner.ownershipRepo.Save(ownership); err != nil {
		t.Fatalf("Save ownership: %v", err)
	}

	err = runner.StartServiceWithActor(context.Background(), "owned-start", "other-session")
	if err == nil {
		t.Fatal("expected non-owner start to be rejected")
	}
	if !strings.Contains(err.Error(), "requires explicit takeover") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_StartServiceWithActor_AllowsNonOwnerAfterOwnerStops(t *testing.T) {
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
						Name:    "start-reacquire",
						Command: "sleep 30",
						HealthCheck: HealthCheck{
							URL:     srv.URL,
							Timeout: 2,
							Retries: 2,
							Backoff: Backoff{Initial: 0.1, Max: 0.2, Multiplier: 1.5},
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	stubNoPortListenersForTest(runner)
	store.Update("start-reacquire", StatusStopped, "")

	if err := runner.StartServiceWithActor(context.Background(), "start-reacquire", "owner-session"); err != nil {
		t.Fatalf("initial StartServiceWithActor: %v", err)
	}

	if err := runner.StopServiceWithActor(context.Background(), "start-reacquire", "owner-session"); err != nil {
		t.Fatalf("StopServiceWithActor: %v", err)
	}

	if err := runner.StartServiceWithActor(context.Background(), "start-reacquire", "other-session"); err != nil {
		t.Fatalf("non-owner should be able to start after stopped owner cleanup, got: %v", err)
	}

	ownership, err := runner.ownershipRepo.FindByServiceName("start-reacquire")
	if err != nil {
		t.Fatalf("FindByServiceName: %v", err)
	}
	if ownership.OwnerSessionID != "other-session" {
		t.Fatalf("owner session = %q, want other-session", ownership.OwnerSessionID)
	}

	if err := runner.StopServiceWithActor(context.Background(), "start-reacquire", "other-session"); err != nil {
		t.Fatalf("cleanup StopServiceWithActor: %v", err)
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

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	if pid <= 0 {
		t.Fatalf("invalid pid %d", pid)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			if err == syscall.ESRCH {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pid %d still alive after %s", pid, timeout)
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

func TestStartAndCheck_ClassifiesReadinessTimeout(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"readiness-timeout-svc"})

	cfg := &Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:    "readiness-timeout-svc",
					Command: "sleep 30",
					HealthCheck: HealthCheck{
						URL:     "http://127.0.0.1:1/health",
						Timeout: 1,
						Retries: 1,
						Backoff: Backoff{Initial: 0.1, Max: 0.1, Multiplier: 1},
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

	node := &ServiceNode{Service: cfg.Flatten()[0]}
	err := runner.startAndCheck(context.Background(), node)
	if err == nil {
		t.Fatal("expected readiness failure")
	}

	status := store.Get("readiness-timeout-svc")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.Status != StatusFailed {
		t.Fatalf("status = %s, want %s", status.Status, StatusFailed)
	}
	if status.Phase != domain.ServiceLifecyclePhaseReadiness {
		t.Fatalf("phase = %q, want %q", status.Phase, domain.ServiceLifecyclePhaseReadiness)
	}
	if status.FailurePhase != domain.ServiceLifecyclePhaseReadiness {
		t.Fatalf("failure_phase = %q, want %q", status.FailurePhase, domain.ServiceLifecyclePhaseReadiness)
	}
	if status.FailureCode != domain.ServiceFailureCodeReadinessTimeout {
		t.Fatalf("failure_code = %q, want %q", status.FailureCode, domain.ServiceFailureCodeReadinessTimeout)
	}

	runner.stopProcess("readiness-timeout-svc")
}

func TestStartAndCheck_ClassifiesBadReadinessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	store := NewStatusStore()
	store.Init([]string{"bad-readiness-svc"})

	cfg := &Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:    "bad-readiness-svc",
					Command: "sleep 30",
					HealthCheck: HealthCheck{
						URL:     srv.URL,
						Timeout: 1,
						Retries: 1,
						Backoff: Backoff{Initial: 0.1, Max: 0.1, Multiplier: 1},
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

	node := &ServiceNode{Service: cfg.Flatten()[0]}
	err := runner.startAndCheck(context.Background(), node)
	if err == nil {
		t.Fatal("expected bad readiness status failure")
	}

	status := store.Get("bad-readiness-svc")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.Status != StatusFailed {
		t.Fatalf("status = %s, want %s", status.Status, StatusFailed)
	}
	if status.Phase != domain.ServiceLifecyclePhaseReadiness {
		t.Fatalf("phase = %q, want %q", status.Phase, domain.ServiceLifecyclePhaseReadiness)
	}
	if status.FailureCode != domain.ServiceFailureCodeBadReadiness {
		t.Fatalf("failure_code = %q, want %q", status.FailureCode, domain.ServiceFailureCodeBadReadiness)
	}

	runner.stopProcess("bad-readiness-svc")
}

func TestStartAndCheck_ClassifiesLaunchProcessExited(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"launch-fail-svc"})

	cfg := &Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:       "launch-fail-svc",
					Command:    "sleep 30",
					WorkingDir: "/path/does/not/exist",
					HealthCheck: HealthCheck{
						URL:     "http://127.0.0.1:1/health",
						Timeout: 1,
						Retries: 1,
						Backoff: Backoff{Initial: 0.1, Max: 0.1, Multiplier: 1},
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

	node := &ServiceNode{Service: cfg.Flatten()[0]}
	err := runner.startAndCheck(context.Background(), node)
	if err == nil {
		t.Fatal("expected launch failure")
	}

	status := store.Get("launch-fail-svc")
	if status == nil {
		t.Fatal("status should exist")
	}
	if status.Status != StatusFailed {
		t.Fatalf("status = %s, want %s", status.Status, StatusFailed)
	}
	if status.Phase != domain.ServiceLifecyclePhaseLaunch {
		t.Fatalf("phase = %q, want %q", status.Phase, domain.ServiceLifecyclePhaseLaunch)
	}
	if status.FailureCode != domain.ServiceFailureCodeProcessExited {
		t.Fatalf("failure_code = %q, want %q", status.FailureCode, domain.ServiceFailureCodeProcessExited)
	}
}

func TestStartAndCheck_ContextCancellationNotClassifiedAsReadinessTimeout(t *testing.T) {
	t.Run("context canceled", func(t *testing.T) {
		store := NewStatusStore()
		store.Init([]string{"ctx-canceled-svc"})

		cfg := &Config{
			Version: "1",
			Groups: []Group{
				{Name: "g1", Services: []Service{
					{
						Name:    "ctx-canceled-svc",
						Command: "sleep 30",
						HealthCheck: HealthCheck{
							URL:     "http://127.0.0.1:1/health",
							Timeout: 5,
							Retries: 5,
							Backoff: Backoff{Initial: 0.1, Max: 0.1, Multiplier: 1},
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
		cancel()

		node := &ServiceNode{Service: cfg.Flatten()[0]}
		err := runner.startAndCheck(ctx, node)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got: %v", err)
		}

		status := store.Get("ctx-canceled-svc")
		if status == nil {
			t.Fatal("status should exist")
		}
		if status.FailureCode == domain.ServiceFailureCodeReadinessTimeout {
			t.Fatalf("failure_code should not be readiness timeout, got %q", status.FailureCode)
		}
		if status.FailureCode != "" {
			t.Fatalf("failure_code should be empty for context cancellation, got %q", status.FailureCode)
		}
		if status.FailurePhase != "" {
			t.Fatalf("failure_phase should be empty for context cancellation, got %q", status.FailurePhase)
		}

		runner.stopProcess("ctx-canceled-svc")
	})

	t.Run("deadline exceeded", func(t *testing.T) {
		store := NewStatusStore()
		store.Init([]string{"ctx-deadline-svc"})

		cfg := &Config{
			Version: "1",
			Groups: []Group{
				{Name: "g1", Services: []Service{
					{
						Name:    "ctx-deadline-svc",
						Command: "sleep 30",
						HealthCheck: HealthCheck{
							URL:     "http://127.0.0.1:1/health",
							Timeout: 5,
							Retries: 5,
							Backoff: Backoff{Initial: 0.1, Max: 0.1, Multiplier: 1},
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

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		time.Sleep(20 * time.Millisecond)

		node := &ServiceNode{Service: cfg.Flatten()[0]}
		err := runner.startAndCheck(ctx, node)
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got: %v", err)
		}

		status := store.Get("ctx-deadline-svc")
		if status == nil {
			t.Fatal("status should exist")
		}
		if status.FailureCode == domain.ServiceFailureCodeReadinessTimeout {
			t.Fatalf("failure_code should not be readiness timeout, got %q", status.FailureCode)
		}
		if status.FailureCode != "" {
			t.Fatalf("failure_code should be empty for deadline exceeded, got %q", status.FailureCode)
		}
		if status.FailurePhase != "" {
			t.Fatalf("failure_phase should be empty for deadline exceeded, got %q", status.FailurePhase)
		}

		runner.stopProcess("ctx-deadline-svc")
	})
}

func TestRunner_StartServiceWithActor_RunsPreflightBeforeLaunch(t *testing.T) {
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
						Name:    "preflight-before-launch",
						Command: "sleep 30",
						HealthCheck: HealthCheck{
							URL:     srv.URL,
							Timeout: 2,
							Retries: 2,
							Backoff: Backoff{Initial: 0.1, Max: 0.2, Multiplier: 1.5},
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("preflight-before-launch", StatusStopped, "")

	preflightCalled := false
	runner.preflightFn = func(ctx context.Context, svc Service) error {
		preflightCalled = true
		return nil
	}

	if err := runner.StartServiceWithActor(context.Background(), "preflight-before-launch", "ui-session"); err != nil {
		t.Fatalf("StartServiceWithActor: %v", err)
	}
	if !preflightCalled {
		t.Fatal("expected preflight before manual launch")
	}

	if err := runner.StopServiceWithActor(context.Background(), "preflight-before-launch", "ui-session"); err != nil {
		t.Fatalf("cleanup StopServiceWithActor: %v", err)
	}
}

func TestRunner_StartServiceWithActor_CleansForeignPortConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "platform",
				Services: []Service{
					{
						Name:    "git-oauth",
						Command: "sleep 30",
						HealthCheck: HealthCheck{
							URL:     srv.URL,
							Timeout: 2,
							Retries: 2,
							Backoff: Backoff{Initial: 0.1, Max: 0.2, Multiplier: 1.5},
						},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("git-oauth", StatusStopped, "")

	calls := 0
	healthPort := domain.ResolveHealthPort(srv.URL)
	runner.listenerPIDsFn = func(port string) ([]int, error) {
		if port != healthPort {
			return nil, nil
		}
		calls++
		if calls == 1 {
			return []int{8765}, nil
		}
		return nil, nil
	}

	if err := runner.StartServiceWithActor(context.Background(), "git-oauth", "ui-session"); err != nil {
		t.Fatalf("StartServiceWithActor after conflict cleanup: %v", err)
	}

	status := store.Get("git-oauth")
	if status == nil || status.Status != StatusHealthy {
		t.Fatalf("expected healthy git-oauth, got %+v", status)
	}

	if err := runner.StopServiceWithActor(context.Background(), "git-oauth", "ui-session"); err != nil {
		t.Fatalf("cleanup StopServiceWithActor: %v", err)
	}
}
