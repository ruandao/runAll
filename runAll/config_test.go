package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_ValidYAML(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: infra
    services:
      - name: redis
        command: "redis-server"
        health_check:
          url: "http://localhost:6379"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != "1" {
		t.Errorf("version = %q, want %q", cfg.Version, "1")
	}
	if len(cfg.Groups) != 1 {
		t.Fatalf("groups len = %d, want 1", len(cfg.Groups))
	}
	svc := cfg.Groups[0].Services[0]
	if svc.Name != "redis" {
		t.Errorf("service name = %q, want %q", svc.Name, "redis")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: infra
    services:
      - name: svc
        command: "echo hi"
        health_check:
          url: "http://localhost:8080"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svc := cfg.Groups[0].Services[0]
	if svc.OnFailure != "exit" {
		t.Errorf("on_failure default = %q, want %q", svc.OnFailure, "exit")
	}
	if svc.HealthCheck.Timeout != 30 {
		t.Errorf("timeout default = %d, want 30", svc.HealthCheck.Timeout)
	}
	if svc.HealthCheck.Retries != 10 {
		t.Errorf("retries default = %d, want 10", svc.HealthCheck.Retries)
	}
	if svc.HealthCheck.Backoff.Initial != 1.0 {
		t.Errorf("backoff.initial default = %f, want 1.0", svc.HealthCheck.Backoff.Initial)
	}
	if svc.HealthCheck.Backoff.Max != 8.0 {
		t.Errorf("backoff.max default = %f, want 8.0", svc.HealthCheck.Backoff.Max)
	}
	if svc.HealthCheck.Backoff.Multiplier != 2.0 {
		t.Errorf("backoff.multiplier default = %f, want 2.0", svc.HealthCheck.Backoff.Multiplier)
	}
}

func TestLoadConfig_DuplicateServiceNames(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: g1
    services:
      - name: dup
        command: "a"
        health_check:
          url: "http://localhost:1"
  - name: g2
    services:
      - name: dup
        command: "b"
        health_check:
          url: "http://localhost:2"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for duplicate service names")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_MissingDependsOn(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: g1
    services:
      - name: svc
        command: "a"
        health_check:
          url: "http://localhost:1"
        depends_on: [nonexistent]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing depends_on reference")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_InvalidOnFailure(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: g1
    services:
      - name: svc
        command: "a"
        health_check:
          url: "http://localhost:1"
        on_failure: panic
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid on_failure")
	}
	if !strings.Contains(err.Error(), "on_failure") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFlattenServices(t *testing.T) {
	cfg := &Config{
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{Name: "a"}, {Name: "b"},
			}},
			{Name: "g2", Services: []Service{
				{Name: "c"},
			}},
		},
	}
	got := cfg.Flatten()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	expected := []string{"a", "b", "c"}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("names[%d] = %q, want %q", i, n, expected[i])
		}
	}
}

func TestLoadConfig_MissingCommand(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: g1
    services:
      - name: svc
        health_check:
          url: "http://localhost:1"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_MissingHealthCheckURL(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: g1
    services:
      - name: svc
        command: "echo hi"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing health_check.url")
	}
	if !strings.Contains(err.Error(), "health_check") {
		t.Fatalf("unexpected error: %v", err)
	}
}
