package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func waitHealthy(ctx context.Context, cfg HealthCheck) error {
	interval := time.Duration(cfg.Backoff.Initial * float64(time.Second))
	maxInterval := time.Duration(cfg.Backoff.Max * float64(time.Second))

	deadline := time.Now().Add(time.Duration(cfg.Timeout) * time.Second)

	for i := 0; i < cfg.Retries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("health check timed out after %ds", cfg.Timeout)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 400 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}

		interval = time.Duration(float64(interval) * cfg.Backoff.Multiplier)
		if interval > maxInterval {
			interval = maxInterval
		}
	}

	return fmt.Errorf("health check failed after %d retries", cfg.Retries)
}
