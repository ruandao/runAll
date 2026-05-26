package main

import (
	"context"
	"strings"
	"testing"

	"runAll/src/domain"
)

func TestPreflightService_CleansForeignListenerViaDomainService(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "platform",
				Services: []Service{
					{
						Name:        "git-oauth",
						Command:     "./run.sh",
						HealthCheck: HealthCheck{URL: "http://127.0.0.1:8002/api/health/"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	calls := 0
	runner.listenerPIDsFn = func(port string) ([]int, error) {
		if port != "8002" {
			return nil, nil
		}
		calls++
		if calls == 1 {
			return []int{4242}, nil
		}
		return nil, nil
	}

	svc := runner.findService("git-oauth")
	if svc == nil {
		t.Fatal("git-oauth service not found")
	}
	if err := runner.preflightService(context.Background(), *svc); err != nil {
		t.Fatalf("preflightService: %v", err)
	}
}

func TestPreflightService_RecordsPortConflictWhenCleanupFails(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "platform",
				Services: []Service{
					{
						Name:        "git-oauth",
						Command:     "./run.sh",
						HealthCheck: HealthCheck{URL: "http://127.0.0.1:8002/api/health/"},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	runner.listenerPIDsFn = func(port string) ([]int, error) {
		if port == "8002" {
			return []int{4242}, nil
		}
		return nil, nil
	}

	svc := runner.findService("git-oauth")
	if svc == nil {
		t.Fatal("git-oauth service not found")
	}
	err = runner.preflightService(context.Background(), *svc)
	if err == nil {
		t.Fatal("expected port conflict when foreign listener persists")
	}
	if !strings.Contains(err.Error(), domain.ServiceFailureCodePortConflict) {
		t.Fatalf("expected structured port conflict, got: %v", err)
	}
	status := store.Get("git-oauth")
	if status == nil || !strings.Contains(status.Error, domain.ServiceFailureCodePortConflict) {
		t.Fatalf("expected preflight failure recorded, got %+v", status)
	}
}
