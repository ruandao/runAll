package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"runAll/src/domain"
)

func TestLoadRuntimeConfig_UsesSourceGuard(t *testing.T) {
	original := loadConfigWithSourceGuardFn
	t.Cleanup(func() {
		loadConfigWithSourceGuardFn = original
	})
	t.Setenv("RUNALL_SECONDARY_CONFIG", "")

	baseDir := t.TempDir()
	primaryPath := filepath.Join(baseDir, "runAll", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(primaryPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(primaryPath, []byte("version: '1'\ngroups: []\n"), 0o644); err != nil {
		t.Fatalf("write primary: %v", err)
	}

	called := false
	loadConfigWithSourceGuardFn = func(primaryPath, secondaryPath string) (*Config, domain.ConfigFingerprint, error) {
		called = true
		wantPrimary := filepath.Join(baseDir, "runAll", "config.yaml")
		if primaryPath != wantPrimary {
			t.Fatalf("primaryPath = %q, want %q", primaryPath, wantPrimary)
		}
		if secondaryPath != "" {
			t.Fatalf("secondaryPath = %q, want empty", secondaryPath)
		}
		return &Config{Version: "1"}, domain.ConfigFingerprint{}, nil
	}

	cfg, err := loadRuntimeConfig(primaryPath)
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}
	if !called {
		t.Fatal("expected loadRuntimeConfig to call LoadConfigWithSourceGuard")
	}
	if cfg == nil || cfg.Version != "1" {
		t.Fatalf("cfg = %#v, want version 1", cfg)
	}
}

func TestLoadRuntimeConfig_FailsWhenPrimarySecondaryMismatch(t *testing.T) {
	original := loadConfigWithSourceGuardFn
	t.Cleanup(func() {
		loadConfigWithSourceGuardFn = original
	})
	loadConfigWithSourceGuardFn = LoadConfigWithSourceGuard

	baseDir := t.TempDir()
	primaryPath := filepath.Join(baseDir, "runAll", "config.yaml")
	secondaryPath := filepath.Join(baseDir, "runAll.yaml")
	t.Setenv("RUNALL_SECONDARY_CONFIG", secondaryPath)
	if err := os.MkdirAll(filepath.Dir(primaryPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(primaryPath, []byte("version: '1'\ngroups: []\n"), 0o644); err != nil {
		t.Fatalf("write primary: %v", err)
	}
	if err := os.WriteFile(secondaryPath, []byte("version: '2'\ngroups: []\n"), 0o644); err != nil {
		t.Fatalf("write secondary: %v", err)
	}

	_, err := loadRuntimeConfig(primaryPath)
	if err == nil {
		t.Fatal("expected loadRuntimeConfig mismatch error")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "config source mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}
