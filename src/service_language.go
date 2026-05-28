package main

import (
	"os"
	"path/filepath"
	"strings"
)

func detectServiceLanguage(svc Service) string {
	if lang := strings.TrimSpace(svc.Language); lang != "" {
		return lang
	}
	if lang := inferLanguageFromCommand(svc.Command); lang != "" {
		return lang
	}
	return inferLanguageFromWorkingDir(svc.WorkingDir)
}

func inferLanguageFromCommand(command string) string {
	cmd := strings.ToLower(command)
	switch {
	case strings.Contains(cmd, "docker compose"), strings.Contains(cmd, "docker-compose"), strings.Contains(cmd, "docker "):
		return "Docker"
	case strings.Contains(cmd, "python"), strings.Contains(cmd, "manage.py"), strings.Contains(cmd, "django"):
		return "Python"
	case strings.Contains(cmd, "npm "), strings.Contains(cmd, "node "), strings.Contains(cmd, "vite"):
		return "JavaScript"
	case strings.Contains(cmd, "go build"), strings.Contains(cmd, "go run"):
		return "Go"
	default:
		return ""
	}
}

func inferLanguageFromWorkingDir(workingDir string) string {
	if workingDir == "" {
		return "—"
	}
	if lang := languageFromDirMarkers(workingDir); lang != "" {
		return lang
	}
	for _, sub := range []string{"Saas_project", "src"} {
		if lang := languageFromDirMarkers(filepath.Join(workingDir, sub)); lang != "" {
			return lang
		}
	}
	return "—"
}

func languageFromDirMarkers(dir string) string {
	if dir == "" {
		return ""
	}
	if fileExists(filepath.Join(dir, "go.mod")) {
		return "Go"
	}
	if fileExists(filepath.Join(dir, "package.json")) {
		return "JavaScript"
	}
	if fileExists(filepath.Join(dir, "manage.py")) || fileExists(filepath.Join(dir, "pyproject.toml")) {
		return "Python"
	}
	if fileExists(filepath.Join(dir, "docker-compose.yml")) || fileExists(filepath.Join(dir, "docker-compose.yaml")) {
		return "Docker"
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
