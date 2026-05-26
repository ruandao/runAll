package main

import (
	"strings"
	"testing"
)

func makeService(name string, deps ...string) Service {
	return Service{
		Name:        name,
		Command:     "echo " + name,
		HealthCheck: HealthCheck{URL: "http://localhost/" + name},
		DependsOn:   deps,
	}
}

func TestBuildDAG_NoDependencies(t *testing.T) {
	services := []Service{
		makeService("a"),
		makeService("b"),
		makeService("c"),
	}
	levels, err := BuildDAG(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(levels) != 1 {
		t.Fatalf("levels = %d, want 1", len(levels))
	}
	if len(levels[0].Services) != 3 {
		t.Fatalf("level 0 size = %d, want 3", len(levels[0].Services))
	}
}

func TestBuildDAG_LinearChain(t *testing.T) {
	services := []Service{
		makeService("c", "b"),
		makeService("b", "a"),
		makeService("a"),
	}
	levels, err := BuildDAG(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(levels))
	}
	if levels[0].Services[0].Service.Name != "a" {
		t.Errorf("level 0 = %q, want 'a'", levels[0].Services[0].Service.Name)
	}
	if levels[1].Services[0].Service.Name != "b" {
		t.Errorf("level 1 = %q, want 'b'", levels[1].Services[0].Service.Name)
	}
	if levels[2].Services[0].Service.Name != "c" {
		t.Errorf("level 2 = %q, want 'c'", levels[2].Services[0].Service.Name)
	}
}

func TestBuildDAG_DiamondDependency(t *testing.T) {
	// a -> b, a -> c, b -> d, c -> d
	services := []Service{
		makeService("a"),
		makeService("b", "a"),
		makeService("c", "a"),
		makeService("d", "b", "c"),
	}
	levels, err := BuildDAG(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(levels))
	}
	// Level 0: a
	// Level 1: b, c (parallel)
	// Level 2: d
	if len(levels[0].Services) != 1 || levels[0].Services[0].Service.Name != "a" {
		t.Errorf("level 0: want [a]")
	}
	if len(levels[1].Services) != 2 {
		t.Errorf("level 1 size = %d, want 2", len(levels[1].Services))
	}
	if len(levels[2].Services) != 1 || levels[2].Services[0].Service.Name != "d" {
		t.Errorf("level 2: want [d]")
	}
}

func TestBuildDAG_CycleDetection(t *testing.T) {
	services := []Service{
		makeService("a", "b"),
		makeService("b", "a"),
	}
	_, err := BuildDAG(services)
	if err == nil {
		t.Fatal("expected error for cyclic dependency")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention 'cycle', got: %v", err)
	}
}

func TestBuildDAG_SelfReference(t *testing.T) {
	services := []Service{
		makeService("a", "a"),
	}
	_, err := BuildDAG(services)
	if err == nil {
		t.Fatal("expected error for self-referencing dependency")
	}
}

func TestBuildDAG_MissingDependency(t *testing.T) {
	services := []Service{
		makeService("a", "nonexistent"),
	}
	_, err := BuildDAG(services)
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
	if !strings.Contains(err.Error(), "unknown service") {
		t.Fatalf("error should mention unknown service, got: %v", err)
	}
}

func TestBuildDAG_EmptyList(t *testing.T) {
	levels, err := BuildDAG([]Service{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(levels) != 0 {
		t.Fatalf("levels = %d, want 0", len(levels))
	}
}

func TestBuildDAG_ThreeLevels(t *testing.T) {
	services := []Service{
		makeService("frontend", "backend"),
		makeService("backend", "db", "cache"),
		makeService("db"),
		makeService("cache"),
	}
	levels, err := BuildDAG(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(levels))
	}
	// Level 0: db, cache
	// Level 1: backend
	// Level 2: frontend
}
