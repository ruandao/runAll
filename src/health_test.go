package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWaitHealthy_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := HealthCheck{
		URL:     srv.URL,
		Timeout: 5,
		Retries: 3,
		Backoff: Backoff{Initial: 0.001, Max: 0.01, Multiplier: 2.0},
	}

	ctx := context.Background()
	err := waitHealthy(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitHealthy_RetryThenSuccess(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := HealthCheck{
		URL:     srv.URL,
		Timeout: 5,
		Retries: 5,
		Backoff: Backoff{Initial: 0.001, Max: 0.01, Multiplier: 2.0},
	}

	ctx := context.Background()
	err := waitHealthy(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestWaitHealthy_ExhaustRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := HealthCheck{
		URL:     srv.URL,
		Timeout: 2,
		Retries: 3,
		Backoff: Backoff{Initial: 0.001, Max: 0.01, Multiplier: 2.0},
	}

	ctx := context.Background()
	err := waitHealthy(ctx, cfg)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
}

func TestWaitHealthy_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := HealthCheck{
		URL:     srv.URL,
		Timeout: 60,
		Retries: 100,
		Backoff: Backoff{Initial: 0.5, Max: 5.0, Multiplier: 2.0},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := waitHealthy(ctx, cfg)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestCheckHealth_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := context.Background()
	err := checkHealth(ctx, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckHealth_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx := context.Background()
	err := checkHealth(ctx, srv.URL)
	if err == nil {
		t.Fatal("expected error for 503")
	}
}

func TestCheckHealth_ConnectionRefused(t *testing.T) {
	ctx := context.Background()
	err := checkHealth(ctx, "http://127.0.0.1:1/health")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestCheckHealth_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := checkHealth(ctx, srv.URL)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}
