package main

import (
	"testing"
)

func TestStatusStore_UpdateAndGet(t *testing.T) {
	store := NewStatusStore()
	names := []string{"a", "b"}
	store.Init(names)

	store.Update("a", StatusStarting, "")
	store.Update("b", StatusHealthy, "")

	all := store.All()
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}

	a := store.Get("a")
	if a.Status != StatusStarting {
		t.Errorf("a status = %q, want %q", a.Status, StatusStarting)
	}

	b := store.Get("b")
	if b.Status != StatusHealthy {
		t.Errorf("b status = %q, want %q", b.Status, StatusHealthy)
	}
}

func TestStatusStore_InitSetsPending(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"redis", "kafka"})

	for _, name := range []string{"redis", "kafka"} {
		s := store.Get(name)
		if s.Status != StatusPending {
			t.Errorf("%s status = %q, want %q", name, s.Status, StatusPending)
		}
	}
}

func TestStatusStore_UpdatePreservesFields(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"test-svc"})

	store.Update("test-svc", StatusStarting, "")
	store.Update("test-svc", StatusStarting, "") // Idempotent check

	got := store.Get("test-svc")
	if got.Status != StatusStarting {
		t.Errorf("status = %q, want %q", got.Status, StatusStarting)
	}
}

func TestStatusStore_MarkError(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"fail-svc"})

	store.Update("fail-svc", StatusFailed, "connection refused")

	got := store.Get("fail-svc")
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want %q", got.Status, StatusFailed)
	}
	if got.Error != "connection refused" {
		t.Errorf("error = %q, want %q", got.Error, "connection refused")
	}
}

func TestStatusStore_SetDependsOn(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"backend", "frontend"})
	store.SetDependsOn("frontend", []DepStatus{
		{Name: "backend", Status: StatusHealthy},
	})

	got := store.Get("frontend")
	if len(got.DependsOn) != 1 {
		t.Fatalf("depends_on len = %d, want 1", len(got.DependsOn))
	}
	if got.DependsOn[0].Name != "backend" || got.DependsOn[0].Status != StatusHealthy {
		t.Errorf("depends_on[0] = %+v, want {backend healthy}", got.DependsOn[0])
	}
}
