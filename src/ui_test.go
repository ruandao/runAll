package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runAll/src/domain"
)

func TestAPIStatus(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"redis", "kafka"})
	store.Update("redis", StatusHealthy, "")
	store.Update("kafka", StatusStarting, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, nil)

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

func TestAPIStatus_IncludesPortFields(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-port",
					Command:     "npm run dev --port 3000",
					HealthCheck: HealthCheck{URL: "http://localhost:8080/health"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if runner == nil {
		t.Fatal("runner is nil")
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result []*ServiceStatus
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].HealthPort != "8080" {
		t.Fatalf("health_port = %q, want 8080", result[0].HealthPort)
	}
	if result[0].CommandPort != "3000" {
		t.Fatalf("command_port = %q, want 3000", result[0].CommandPort)
	}
}

func TestUIHomePage(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"svc"})

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, nil)

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
	body := rec.Body.String()
	requiredSnippets := []string{
		`data-action="build"`,
		`data-action="restart"`,
		`data-action="logs"`,
		`data-action="clear-logs"`,
		`id="logs-modal-refresh"`,
		`/api/build`,
		`/api/logs`,
		`/api/logs/clear`,
		`svc.health_port || '-'`,
		`svc.command_port || '-'`,
		`health/command:`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(body, snippet) {
			t.Fatalf("home page missing required snippet %q", snippet)
		}
	}
}

func TestAPIBuild_Success(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "svc-build",
					Command:      "echo running",
					BuildCommand: "echo built",
					HealthCheck:  HealthCheck{URL: "http://localhost:9999"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("svc-build", StatusHealthy, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(`{"name":"svc-build"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if response["status"] != "ok" {
		t.Fatalf("status body = %#v, want status=ok", response)
	}
}

func TestAPIBuild_BadRequest(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "svc-build-bad",
					Command:      "echo running",
					BuildCommand: "echo built",
					HealthCheck:  HealthCheck{URL: "http://localhost:9998"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("svc-build-bad", StatusHealthy, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	tests := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{`},
		{name: "missing name", body: `{}`},
		{name: "unknown service", body: `{"name":"missing-service"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertJSONErrorResponse(t, rec, http.StatusBadRequest)
		})
	}
}

func TestAPILogs_Success(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-logs",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9997"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	entry1, err := domain.NewLogEntry(time.Now(), "svc-logs", domain.StreamStdout, "line-1")
	if err != nil {
		t.Fatalf("NewLogEntry: %v", err)
	}
	entry2, err := domain.NewLogEntry(time.Now(), "svc-logs", domain.StreamStderr, "line-2")
	if err != nil {
		t.Fatalf("NewLogEntry: %v", err)
	}
	entry3, err := domain.NewLogEntry(time.Now(), "svc-logs", domain.StreamStdout, "line-3")
	if err != nil {
		t.Fatalf("NewLogEntry: %v", err)
	}
	runner.logRepository.Append("svc-logs", entry1)
	runner.logRepository.Append("svc-logs", entry2)
	runner.logRepository.Append("svc-logs", entry3)

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodGet, "/api/logs?name=svc-logs&lines=2", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Name  string            `json:"name"`
		Lines []domain.LogEntry `json:"lines"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if response.Name != "svc-logs" {
		t.Fatalf("name = %q, want svc-logs", response.Name)
	}
	if len(response.Lines) != 2 {
		t.Fatalf("lines count = %d, want 2", len(response.Lines))
	}
	if response.Lines[0].Message != "line-2" || response.Lines[1].Message != "line-3" {
		t.Fatalf("unexpected log lines: %#v", response.Lines)
	}
}

func TestAPILogs_BadRequest(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-logs-bad",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9996"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	tests := []struct {
		name string
		url  string
	}{
		{name: "missing name", url: "/api/logs?lines=10"},
		{name: "missing lines", url: "/api/logs?name=svc-logs-bad"},
		{name: "invalid lines", url: "/api/logs?name=svc-logs-bad&lines=abc"},
		{name: "non-positive lines", url: "/api/logs?name=svc-logs-bad&lines=0"},
		{name: "unknown service", url: "/api/logs?name=missing-service&lines=10"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertJSONErrorResponse(t, rec, http.StatusBadRequest)
		})
	}
}

func TestAPILogsClear_Success(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-logs-clear",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9981"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	entry, err := domain.NewLogEntry(time.Now(), "svc-logs-clear", domain.StreamStdout, "line-before-clear")
	if err != nil {
		t.Fatalf("NewLogEntry: %v", err)
	}
	runner.logRepository.Append("svc-logs-clear", entry)

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/logs/clear", strings.NewReader(`{"name":"svc-logs-clear"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if response["status"] != "ok" {
		t.Fatalf("status body = %#v, want status=ok", response)
	}
	if got := runner.logRepository.Tail("svc-logs-clear", 10); len(got) != 0 {
		t.Fatalf("remaining logs = %d, want 0", len(got))
	}
}

func TestAPILogsClear_BadRequest(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-logs-clear-bad",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9980"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/logs/clear", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusMethodNotAllowed, "method not allowed")
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/logs/clear", strings.NewReader(`{`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "invalid json")
	})

	t.Run("missing name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/logs/clear", strings.NewReader(`{"name":"   "}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "name is required")
	})

	t.Run("unknown service", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/logs/clear", strings.NewReader(`{"name":"missing-service"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, `service "missing-service" not found`)
	})

	t.Run("runner nil", func(t *testing.T) {
		muxWithNilRunner := http.NewServeMux()
		registerUIHandlers(muxWithNilRunner, store, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/logs/clear", strings.NewReader(`{"name":"svc-logs-clear-bad"}`))
		rec := httptest.NewRecorder()
		muxWithNilRunner.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "runner is required")
	})

	t.Run("log repository nil", func(t *testing.T) {
		runnerWithNilRepo, err := NewRunner(&Config{
			Version: "1",
			Groups: []Group{
				{Name: "g1", Services: []Service{
					{
						Name:        "svc-logs-clear-bad",
						Command:     "echo running",
						HealthCheck: HealthCheck{URL: "http://localhost:9980"},
					},
				}},
			},
		}, store)
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}
		runnerWithNilRepo.logRepository = nil

		muxWithNilRepo := http.NewServeMux()
		registerUIHandlers(muxWithNilRepo, store, runnerWithNilRepo)

		req := httptest.NewRequest(http.MethodPost, "/api/logs/clear", strings.NewReader(`{"name":"svc-logs-clear-bad"}`))
		rec := httptest.NewRecorder()
		muxWithNilRepo.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "log repository is required")
	})
}

func TestAPIBuild_WhitespaceNameReturnsRequired(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "svc-build-space",
					Command:      "echo running",
					BuildCommand: "echo built",
					HealthCheck:  HealthCheck{URL: "http://localhost:9985"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(`{"name":"   "}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertJSONErrorMessage(t, rec, http.StatusBadRequest, "name is required")
}

func TestAPILogs_WhitespaceNameReturnsRequired(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-logs-space",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9984"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodGet, "/api/logs?name=%20%20%20&lines=50", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertJSONErrorMessage(t, rec, http.StatusBadRequest, "name is required")
}

func TestAPILogs_TooLargeLinesRejected(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-logs-cap",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9983"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodGet, "/api/logs?name=svc-logs-cap&lines=5001", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertJSONErrorMessage(t, rec, http.StatusBadRequest, "lines must be <= 2000")
}

func TestUILogsModal_CloseAndRefreshGuardsPresent(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"svc"})

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, nil)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()

	requiredSnippets := []string{
		"function closeLogsModal()",
		"stopLogsAutoRefresh();",
		"logsState.requestSerial += 1;",
		"if (!logsState.open || !logsState.service)",
		"if (!logsState.open || logsState.service !== name || reqId !== logsState.requestSerial)",
		"if (logsState.open && logsState.service === name)",
		"fetch('/api/logs/clear', {",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(body, snippet) {
			t.Fatalf("status.html missing logs modal guard snippet %q", snippet)
		}
	}
}

func TestAPIBuild_StateConflictReturnsBadRequest(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "svc-build-conflict",
					Command:      "echo running",
					BuildCommand: "echo built",
					HealthCheck:  HealthCheck{URL: "http://localhost:9995"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("svc-build-conflict", StatusStarting, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/build", strings.NewReader(`{"name":"svc-build-conflict"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertJSONErrorResponse(t, rec, http.StatusBadRequest)
}

func TestAPIMethodNotAllowed_ReturnsJSON(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "svc-method",
					Command:      "echo running",
					BuildCommand: "echo built",
					HealthCheck:  HealthCheck{URL: "http://localhost:9982"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "restart get", method: http.MethodGet, path: "/api/restart"},
		{name: "build get", method: http.MethodGet, path: "/api/build"},
		{name: "logs post", method: http.MethodPost, path: "/api/logs"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertJSONErrorMessage(t, rec, http.StatusMethodNotAllowed, "method not allowed")
		})
	}
}

func assertJSONErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	var body map[string]string
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if strings.TrimSpace(body["error"]) == "" {
		t.Fatalf("error body = %#v, want non-empty error", body)
	}
}

func assertJSONErrorMessage(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMessage string) {
	t.Helper()
	assertJSONErrorResponse(t, rec, wantStatus)
	var body map[string]string
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if body["error"] != wantMessage {
		t.Fatalf("error = %q, want %q", body["error"], wantMessage)
	}
}
