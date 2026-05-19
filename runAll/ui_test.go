package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIStatus(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"redis", "kafka"})
	store.Update("redis", StatusHealthy, "")
	store.Update("kafka", StatusStarting, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store)

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result []*ServiceStatus
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
}

func TestUIHomePage(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"svc"})

	mux := http.NewServeMux()
	registerUIHandlers(mux, store)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", contentType)
	}
}
