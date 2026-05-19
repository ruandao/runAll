package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to YAML configuration file")
	daemon := flag.Bool("daemon", false, "Start services and exit (no Web UI)")
	uiPort := flag.String("ui-port", ":9999", "Web UI listen address")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	store := NewStatusStore()
	runner, err := NewRunner(cfg, store)
	if err != nil {
		log.Fatalf("Setup error: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start Web UI (only in foreground mode)
	if !*daemon {
		srv := startUIServer(store, *uiPort)
		log.Printf("Web UI: http://localhost%s", *uiPort)
		defer srv.Close()
	}

	if err := runner.Run(ctx, *daemon); err != nil {
		log.Printf("Exiting due to error: %v", err)
		os.Exit(1)
	}
}
