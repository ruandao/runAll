package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

func waitHealthy(ctx context.Context, cfg HealthCheck) error {
	interval := time.Duration(cfg.Backoff.Initial * float64(time.Second))
	maxInterval := time.Duration(cfg.Backoff.Max * float64(time.Second))

	deadline := time.Now().Add(time.Duration(cfg.Timeout) * time.Second)
	var lastCheckErr error

	for i := 0; i < cfg.Retries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}

		if time.Now().After(deadline) {
			if lastCheckErr != nil {
				return fmt.Errorf("health check timed out after %ds (last error: %v)", cfg.Timeout, lastCheckErr)
			}
			return fmt.Errorf("health check timed out after %ds", cfg.Timeout)
		}

		if err := checkProbe(ctx, cfg); err == nil {
			return nil
		} else {
			lastCheckErr = err
		}

		interval = time.Duration(float64(interval) * cfg.Backoff.Multiplier)
		if interval > maxInterval {
			interval = maxInterval
		}
	}

	return fmt.Errorf("health check failed after %d retries", cfg.Retries)
}

func checkProbe(ctx context.Context, hc HealthCheck) error {
	if hc.UsesTCP() {
		return checkTCP(ctx, strings.TrimSpace(hc.TCP))
	}
	return checkHealth(ctx, hc.URL)
}

func checkTCP(ctx context.Context, addr string) error {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

func checkHealth(ctx context.Context, url string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	return fmt.Errorf("unhealthy: HTTP %d", resp.StatusCode)
}
