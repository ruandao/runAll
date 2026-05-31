package main

import (
	"os"
	"path/filepath"
	"strings"
)

func resolveBuildCommand(svc Service) string {
	if cmd := strings.TrimSpace(svc.BuildCommand); cmd != "" {
		return cmd
	}
	if svc.WorkingDir != "" && fileExists(filepath.Join(svc.WorkingDir, "build.sh")) {
		return "./build.sh"
	}
	if cmd := extractGoBuildCommand(svc.Command); cmd != "" {
		return cmd
	}
	if svc.WorkingDir != "" && fileExists(filepath.Join(svc.WorkingDir, "go.mod")) {
		if cmd := extractGoBuildFromRunScript(svc.WorkingDir); cmd != "" {
			return cmd
		}
	}
	return ""
}

func serviceBuildable(svc *Service) bool {
	if svc == nil {
		return false
	}
	return resolveBuildCommand(*svc) != ""
}

func extractGoBuildCommand(command string) string {
	lower := strings.ToLower(command)
	idx := strings.Index(lower, "go build")
	if idx < 0 {
		return ""
	}
	segment := strings.TrimSpace(command[idx:])
	for _, sep := range []string{" && ", " ; ", ";", " || ", " | ", "|", " & "} {
		if cut := strings.Index(segment, sep); cut >= 0 {
			segment = strings.TrimSpace(segment[:cut])
		}
	}
	if strings.HasPrefix(strings.ToLower(segment), "go build") {
		return segment
	}
	return ""
}

func extractGoBuildFromRunScript(workingDir string) string {
	data, err := os.ReadFile(filepath.Join(workingDir, "run.sh"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "go build") {
			// Skip function-body templates (e.g. taskEvents run.sh with ${name}).
			if strings.Contains(trimmed, "$") {
				continue
			}
			return trimmed
		}
	}
	return ""
}
