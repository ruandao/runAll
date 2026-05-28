package main

import (
	"path/filepath"
	"testing"
)

func TestResolveBuildCommand_ExplicitConfig(t *testing.T) {
	got := resolveBuildCommand(Service{
		BuildCommand: "npm run build",
		Command:      "npm run dev",
	})
	if got != "npm run build" {
		t.Fatalf("resolveBuildCommand explicit = %q, want npm run build", got)
	}
}

func TestResolveBuildCommand_FromBuildScript(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	got := resolveBuildCommand(Service{
		Command:    "./bin/go_relayToTrae",
		WorkingDir: filepath.Join(repoRoot, "go_relayToTrae"),
	})
	if got != "./build.sh" {
		t.Fatalf("resolveBuildCommand go-relay = %q, want ./build.sh", got)
	}
}

func TestResolveBuildCommand_FromGoBuildInCommand(t *testing.T) {
	got := resolveBuildCommand(Service{
		Command:    "go build -o go_run_container . && ./go_run_container",
		WorkingDir: "/tmp/unused",
	})
	if got != "go build -o go_run_container ." {
		t.Fatalf("resolveBuildCommand inline go build = %q", got)
	}
}

func TestResolveBuildCommand_FromRunScript(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{name: "task-auth", dir: "taskAuth", want: "go build -o taskAuth ./src"},
		{name: "task-bill", dir: "taskBill", want: "go build -o taskBill ./src"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveBuildCommand(Service{
				Command:    "bash run.sh",
				WorkingDir: filepath.Join(repoRoot, tc.dir),
			})
			if got != tc.want {
				t.Fatalf("resolveBuildCommand() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveBuildCommand_NotBuildable(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	got := resolveBuildCommand(Service{
		Command:    "./run.sh",
		WorkingDir: filepath.Join(repoRoot, "gitOauth"),
	})
	if got != "" {
		t.Fatalf("resolveBuildCommand git-oauth = %q, want empty", got)
	}
}

func TestServiceBuildable(t *testing.T) {
	if !serviceBuildable(&Service{BuildCommand: "npm run build"}) {
		t.Fatal("expected buildable service with explicit build_command")
	}
	if serviceBuildable(&Service{BuildCommand: "   ", Command: "bash run.sh"}) {
		t.Fatal("expected non-buildable service without resolvable build command")
	}
	if serviceBuildable(nil) {
		t.Fatal("expected nil service to be non-buildable")
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	if !serviceBuildable(&Service{
		Command:    "bash run.sh",
		WorkingDir: filepath.Join(repoRoot, "taskAuth"),
	}) {
		t.Fatal("expected task-auth to be buildable via run.sh inference")
	}
}
