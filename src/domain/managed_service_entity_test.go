package domain

import (
	"strings"
	"testing"
)

func TestManagedService_CanStop(t *testing.T) {
	service, err := NewManagedService("api", "platform", ServiceStatusHealthy, nil)
	if err != nil {
		t.Fatalf("NewManagedService: %v", err)
	}

	if err := service.CanStop(nil); err != nil {
		t.Fatalf("CanStop should allow healthy service without dependents: %v", err)
	}

	err = service.CanStop([]string{"web"})
	if err == nil || !strings.Contains(err.Error(), "active downstream dependencies") {
		t.Fatalf("CanStop should reject active dependents, got: %v", err)
	}

	retrying, err := NewManagedService("api", "platform", ServiceStatusRetrying, nil)
	if err != nil {
		t.Fatalf("NewManagedService: %v", err)
	}
	if err := retrying.CanStop(nil); err != nil {
		t.Fatalf("CanStop should allow retrying service without dependents: %v", err)
	}
}

func TestManagedService_CanStart(t *testing.T) {
	stopped, err := NewManagedService("api", "platform", ServiceStatusStopped, nil)
	if err != nil {
		t.Fatalf("NewManagedService: %v", err)
	}
	if !stopped.CanStart() {
		t.Fatal("stopped service should be startable")
	}

	healthy, err := NewManagedService("api", "platform", ServiceStatusHealthy, nil)
	if err != nil {
		t.Fatalf("NewManagedService: %v", err)
	}
	if healthy.CanStart() {
		t.Fatal("healthy service should not be startable")
	}
}
