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
	if svc.HealthCheck.CheckInterval != 10 {
		t.Errorf("check_interval default = %d, want 10", svc.HealthCheck.CheckInterval)
	}
	if svc.HealthCheck.UnhealthyThreshold != 2 {
		t.Errorf("unhealthy_threshold default = %d, want 2", svc.HealthCheck.UnhealthyThreshold)
	}
}

func TestLoadConfig_LoggingFileRoot(t *testing.T) {
	yamlContent := `
version: "1"
logging:
  file_root: /tmp/custom-runall-logs
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
	os.WriteFile(path, []byte(yamlContent), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Logging.FileRoot != "/tmp/custom-runall-logs" {
		t.Errorf("file_root = %q, want /tmp/custom-runall-logs", cfg.Logging.FileRoot)
	}
}

func TestLoadConfig_LoggingFileRootEnvOverride(t *testing.T) {
	t.Setenv("RUNALL_LOG_ROOT", "/tmp/from-env")
	yamlContent := `
version: "1"
logging:
  file_root: /tmp/from-yaml
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
	os.WriteFile(path, []byte(yamlContent), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Logging.FileRoot != "/tmp/from-env" {
		t.Errorf("file_root = %q, want env override /tmp/from-env", cfg.Logging.FileRoot)
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

func TestLoadConfig_BuildCommand(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: g1
    services:
      - name: svc
        command: "./app"
        build_command: "go build -o app ."
        health_check:
          url: "http://localhost:1"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svc := cfg.Groups[0].Services[0]
	if svc.BuildCommand != "go build -o app ." {
		t.Errorf("build_command = %q, want %q", svc.BuildCommand, "go build -o app .")
	}
}

func TestLoadConfig_BuildCommandDefault(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: g1
    services:
      - name: svc
        command: "./app"
        health_check:
          url: "http://localhost:1"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svc := cfg.Groups[0].Services[0]
	if svc.BuildCommand != "" {
		t.Errorf("build_command default = %q, want empty string", svc.BuildCommand)
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

func TestLoadConfig_FailsWhenDualConfigMismatch(t *testing.T) {
	primaryYAML := `
version: "1"
groups:
  - name: g1
    services:
      - name: svc
        command: "echo primary"
        health_check:
          url: "http://localhost:1"
`
	secondaryYAML := `
version: "1"
groups:
  - name: g1
    services:
      - name: svc
        command: "echo secondary"
        health_check:
          url: "http://localhost:1"
`
	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "config.primary.yaml")
	secondaryPath := filepath.Join(dir, "config.secondary.yaml")
	os.WriteFile(primaryPath, []byte(primaryYAML), 0644)
	os.WriteFile(secondaryPath, []byte(secondaryYAML), 0644)

	_, _, err := LoadConfigWithSourceGuard(primaryPath, secondaryPath)
	if err == nil {
		t.Fatal("expected mismatch error when dual config hashes differ")
	}
	if !strings.Contains(err.Error(), "config source mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_RegressionMatrixFixtures(t *testing.T) {
	yaml := `
version: "1"
groups:
  - name: regression
    services:
      - name: matrix-port-conflict
        command: "python3 -m http.server 28080"
        health_check:
          url: "http://127.0.0.1:28080/health"
      - name: matrix-runtime-prereq-blocked
        command: "docker compose up"
        health_check:
          url: "http://127.0.0.1:28081/health"
      - name: matrix-prereq-repaired-healthy
        command: "docker compose up"
        health_check:
          url: "http://127.0.0.1:28082/health"
      - name: matrix-non-owner-rejected
        command: "echo worker"
        health_check:
          url: "http://127.0.0.1:28083/health"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	services := cfg.Flatten()
	if len(services) != 4 {
		t.Fatalf("services len = %d, want 4", len(services))
	}

	gotNames := map[string]bool{}
	for _, svc := range services {
		gotNames[svc.Name] = true
		if svc.HealthCheck.URL == "" {
			t.Fatalf("service %q health_check.url should not be empty", svc.Name)
		}
	}
	for _, name := range []string{
		"matrix-port-conflict",
		"matrix-runtime-prereq-blocked",
		"matrix-prereq-repaired-healthy",
		"matrix-non-owner-rejected",
	} {
		if !gotNames[name] {
			t.Fatalf("service %q not found in loaded config", name)
		}
	}
}

func TestProductionConfig_VueFrontendHasBuildCommand(t *testing.T) {
	path := filepath.Join("..", "config.yaml")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(%q): %v", path, err)
	}

	var vueFrontend *Service
	for _, svc := range cfg.Flatten() {
		if svc.Name == "vue-frontend" {
			svcCopy := svc
			vueFrontend = &svcCopy
			break
		}
	}
	if vueFrontend == nil {
		t.Fatal("vue-frontend service not found in production config")
	}
	if vueFrontend.BuildCommand == "" {
		t.Fatal("vue-frontend build_command must be configured for UI build action")
	}
	if vueFrontend.BuildCommand != "npm run build" {
		t.Fatalf("build_command = %q, want %q", vueFrontend.BuildCommand, "npm run build")
	}
}
