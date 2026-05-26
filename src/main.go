package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"runAll/src/domain"
	"runAll/src/infrastructure"
)

var loadConfigWithSourceGuardFn func(string, string) (*Config, domain.ConfigFingerprint, error) = LoadConfigWithSourceGuard

func loadRuntimeConfig(path string) (*Config, error) {
	cfg, _, err := loadConfigWithSourceGuardFn(path, resolveSecondaryConfigPath(path))
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func resolveSecondaryConfigPath(primaryPath string) string {
	candidate := strings.TrimSpace(os.Getenv("RUNALL_SECONDARY_CONFIG"))
	if candidate == "" {
		return ""
	}
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func resolveOwnershipStorePath(configPath string) string {
	fromEnv := strings.TrimSpace(os.Getenv("RUNALL_OWNERSHIP_STORE"))
	if fromEnv != "" {
		return fromEnv
	}

	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return filepath.Join(".runall", "ownership.json")
	}
	return filepath.Join(filepath.Dir(absConfigPath), ".runall", "ownership.json")
}

func main() {
	command := flag.String("command", "run", "run|doctor|takeover")
	configPath := flag.String("config", "config.yaml", "Path to YAML configuration file")
	daemon := flag.Bool("daemon", false, "Start services and exit (no Web UI)")
	uiPort := flag.String("ui-port", ":9999", "Web UI listen address")
	serviceName := flag.String("service", "", "Service name for takeover command")
	sessionID := flag.String("session-id", "", "Actor session id for takeover command")
	flag.Parse()

	cfg, err := loadRuntimeConfig(*configPath)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	store := NewStatusStore()
	runner, err := NewRunner(cfg, store)
	if err != nil {
		log.Fatalf("Setup error: %v", err)
	}
	ownershipRepo := infrastructure.NewFileServiceOwnershipRepository(resolveOwnershipStorePath(*configPath))
	runner.ownershipRepo = ownershipRepo
	runner.ownershipGuard = domain.NewServiceOwnershipGuardService(ownershipRepo)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch strings.TrimSpace(*command) {
	case "doctor":
		os.Exit(RunDoctor(ctx, runner, os.Stdout))
	case "takeover":
		if strings.TrimSpace(*serviceName) == "" {
			log.Printf("takeover requires -service")
			os.Exit(doctorExitPreflightFailed)
		}
		if strings.TrimSpace(*sessionID) == "" {
			log.Printf("takeover requires explicit -session-id")
			os.Exit(doctorExitPreflightFailed)
		}
		if err := runner.TakeoverService(*serviceName, *sessionID); err != nil {
			log.Printf("takeover failed: %v", err)
			os.Exit(1)
		}
		return
	case "run":
		// Start Web UI (only in foreground mode)
		if !*daemon {
			srv := startUIServer(store, runner, *uiPort)
			log.Printf("Web UI: http://localhost%s", *uiPort)
			defer srv.Close()
		}
	default:
		log.Printf("unknown command %q, want run|doctor|takeover", *command)
		os.Exit(doctorExitPreflightFailed)
	}

	if err := runner.Run(ctx, *daemon); err != nil {
		log.Printf("Exiting due to error: %v", err)
		os.Exit(1)
	}
}
