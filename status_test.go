package main

import (
	"testing"
	"strings"
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

func TestStatusStore_AllReturnsSortedOrder(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"z", "a", "m"})

	// Call All() multiple times — each result must be sorted by name
	for i := 0; i < 10; i++ {
		all := store.All()
		if len(all) != 3 {
			t.Fatalf("len = %d, want 3", len(all))
		}
		if all[0].Name != "a" || all[1].Name != "m" || all[2].Name != "z" {
			t.Fatalf("iteration %d: order = %v, want [a m z]", i, []string{all[0].Name, all[1].Name, all[2].Name})
		}
	}
}

func TestStatusStore_AllSortedByName(t *testing.T) {
	store := NewStatusStore()
	names := []string{"frontend", "backend", "redis", "kafka", "db"}
	store.Init(names)

	all := store.All()
	if len(all) != 5 {
		t.Fatalf("len = %d, want 5", len(all))
	}
	for i := 1; i < len(all); i++ {
		if strings.Compare(all[i-1].Name, all[i].Name) > 0 {
			t.Errorf("not sorted: %q before %q", all[i-1].Name, all[i].Name)
		}
	}
}
