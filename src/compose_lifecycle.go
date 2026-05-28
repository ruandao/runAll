package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
)

func isDetachLaunchCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if strings.Contains(lower, "run-infra.sh") {
		return true
	}
	if !strings.Contains(lower, "compose") {
		return false
	}
	if !strings.Contains(lower, " up") {
		return false
	}
	return strings.Contains(lower, "-d")
}

func composeStopShellCommand(svc *Service) string {
	if svc == nil {
		return ""
	}
	lower := strings.ToLower(strings.TrimSpace(svc.Command))
	if strings.Contains(lower, "run-infra.sh") {
		return "bash run-infra.sh stop"
	}
	if svc.WorkingDir != "" && fileExists(filepath.Join(svc.WorkingDir, "run-infra.sh")) {
		return "bash run-infra.sh stop"
	}
	return ""
}

func (r *Runner) runComposeStopScript(ctx context.Context, svc *Service) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	shellCmd := composeStopShellCommand(svc)
	if shellCmd == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd)
	if svc.WorkingDir != "" {
		cmd.Dir = svc.WorkingDir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compose stop script failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	log.Printf("[%s] compose stop script completed", svc.Name)
	return nil
}
