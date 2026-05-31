package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestIsDetachLaunchCommand(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{command: "bash run-infra.sh managed", want: true},
		{command: "bash run.sh managed", want: true},
		{command: "docker compose -f docker-compose.yml up -d", want: true},
		{command: "docker-compose up -d", want: true},
		{command: "sleep 30", want: false},
		{command: "docker compose up", want: false},
	}
	for _, tt := range tests {
		if got := isDetachLaunchCommand(tt.command); got != tt.want {
			t.Errorf("isDetachLaunchCommand(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}

func TestComposeStopShellCommand_LegacyInfra(t *testing.T) {
	svc := &Service{Command: "bash run-infra.sh managed", WorkingDir: "task2app/Saas_project"}
	if got := composeStopShellCommand(svc); got != "bash run-infra.sh stop" {
		t.Fatalf("composeStopShellCommand() = %q", got)
	}
}

func TestComposeStopShellCommand_DockerInfraRedis(t *testing.T) {
	svc := &Service{Command: "bash run.sh managed", WorkingDir: "runAll/dockerInfra/redis"}
	if got := composeStopShellCommand(svc); got != "bash run.sh stop" {
		t.Fatalf("composeStopShellCommand() = %q", got)
	}
}

func TestWaitHealthyWithLaunchCheck_AllowsDetachLaunchExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cmd.Wait()

	err := waitHealthyWithLaunchCheck(context.Background(), cmd.Process, HealthCheck{
		URL:     srv.URL,
		Timeout: 5,
		Retries: 5,
		Backoff: Backoff{Initial: 0.05, Max: 0.1, Multiplier: 1.5},
	}, true)
	if err != nil {
		t.Fatalf("waitHealthyWithLaunchCheck(allowLaunchExit=true): %v", err)
	}
}

func TestWaitHealthyWithLaunchCheck_RejectsEarlyExitWithoutDetach(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	<-done

	err := waitHealthyWithLaunchCheck(context.Background(), cmd.Process, HealthCheck{
		URL:     "http://127.0.0.1:1/health",
		Timeout: 1,
		Retries: 1,
		Backoff: Backoff{Initial: 0.05, Max: 0.05, Multiplier: 1},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "launch process exited before readiness") {
		t.Fatalf("expected launch exit error, got: %v", err)
	}
}
