package main

import (
	"path/filepath"
	"testing"
)

func TestInferLanguageFromCommand(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "docker compose up -d", want: "Docker"},
		{command: "python3 manage.py runserver", want: "Python"},
		{command: "npm run dev", want: "JavaScript"},
		{command: "go build -o app .", want: "Go"},
		{command: "bash run.sh", want: ""},
	}
	for _, tc := range tests {
		got := inferLanguageFromCommand(tc.command)
		if got != tc.want {
			t.Fatalf("inferLanguageFromCommand(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestDetectServiceLanguage_ExplicitConfig(t *testing.T) {
	svc := Service{
		Language: "Rust",
		Command:  "npm run dev",
	}
	if got := detectServiceLanguage(svc); got != "Rust" {
		t.Fatalf("detectServiceLanguage explicit = %q, want Rust", got)
	}
}

func TestDetectServiceLanguage_FromWorkingDir(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	tests := []struct {
		name string
		svc  Service
		want string
	}{
		{
			name: "task-auth-go",
			svc: Service{
				Command:    "bash run.sh",
				WorkingDir: filepath.Join(repoRoot, "taskAuth"),
			},
			want: "Go",
		},
		{
			name: "git-oauth-python",
			svc: Service{
				Command:    "./run.sh",
				WorkingDir: filepath.Join(repoRoot, "gitOauth"),
			},
			want: "Python",
		},
		{
			name: "vue-frontend-javascript",
			svc: Service{
				Command:    "npm run dev",
				WorkingDir: filepath.Join(repoRoot, "task2app", "front_project", "app"),
			},
			want: "JavaScript",
		},
		{
			name: "git-service-docker",
			svc: Service{
				Command:    "./run.sh managed",
				WorkingDir: filepath.Join(repoRoot, "gitService"),
			},
			want: "Docker",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectServiceLanguage(tc.svc)
			if got != tc.want {
				t.Fatalf("detectServiceLanguage() = %q, want %q", got, tc.want)
			}
		})
	}
}
