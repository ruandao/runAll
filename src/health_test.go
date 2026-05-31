package main

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestCheckTCP_Healthy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := checkTCP(ctx, addr); err != nil {
		t.Fatalf("checkTCP(%q): %v", addr, err)
	}
}

func TestCheckTCP_Unreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := checkTCP(ctx, "127.0.0.1:1"); err == nil {
		t.Fatal("expected error for closed port")
	}
}

func TestWaitHealthy_TCPProbe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	cfg := HealthCheck{
		TCP:     ln.Addr().String(),
		Timeout: 5,
		Retries: 5,
		Backoff: Backoff{Initial: 0.05, Max: 0.1, Multiplier: 1.5},
	}

	if err := waitHealthy(context.Background(), cfg); err != nil {
		t.Fatalf("waitHealthy tcp: %v", err)
	}
}
