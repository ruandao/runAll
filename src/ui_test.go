package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runAll/src/domain"
)

func assertLifecycleAccepted(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if response["status"] != "accepted" {
		t.Fatalf("status body = %#v, want status=accepted", response)
	}
}

func waitForServiceStatus(t *testing.T, store *StatusStore, name string, want Status, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := store.Get(name)
		if status != nil && status.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	status := store.Get(name)
	t.Fatalf("service %s status = %#v, want %q within %s", name, status, want, timeout)
}

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
	if result[0].Group != "g1" {
		t.Fatalf("group = %q, want g1", result[0].Group)
	}
}

func TestAPIStatus_IncludesBuildableAndLanguage(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:         "svc-buildable",
					Command:      "npm run dev",
					BuildCommand: "npm run build",
					HealthCheck:  HealthCheck{URL: "http://localhost:4000/health"},
				},
				{
					Name:        "svc-no-build",
					Command:     "bash run.sh",
					Language:    "Go",
					HealthCheck: HealthCheck{URL: "http://localhost:8003/api/health/"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}

	byName := map[string]map[string]any{}
	for _, row := range result {
		name, _ := row["name"].(string)
		byName[name] = row
	}

	if byName["svc-buildable"]["buildable"] != true {
		t.Fatalf("svc-buildable buildable = %#v, want true", byName["svc-buildable"]["buildable"])
	}
	if byName["svc-buildable"]["language"] != "JavaScript" {
		t.Fatalf("svc-buildable language = %#v, want JavaScript", byName["svc-buildable"]["language"])
	}
	if byName["svc-no-build"]["buildable"] != false {
		t.Fatalf("svc-no-build buildable = %#v, want false", byName["svc-no-build"]["buildable"])
	}
	if byName["svc-no-build"]["language"] != "Go" {
		t.Fatalf("svc-no-build language = %#v, want Go", byName["svc-no-build"]["language"])
	}
}

func TestUIIncludesFailureFields(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-failure-ui",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:8081/health"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.SetPID("svc-failure-ui", 12345)
	if err := runner.TakeoverService("svc-failure-ui", "session-ui-owner"); err != nil {
		t.Fatalf("TakeoverService: %v", err)
	}
	store.RecordFailure("svc-failure-ui", "readiness", "READINESS_TIMEOUT", "waiting on /health")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0]["failure_phase"] != "readiness" {
		t.Fatalf("failure_phase = %#v, want readiness", result[0]["failure_phase"])
	}
	if result[0]["failure_code"] != "READINESS_TIMEOUT" {
		t.Fatalf("failure_code = %#v, want READINESS_TIMEOUT", result[0]["failure_code"])
	}
	if result[0]["hint"] != "waiting on /health" {
		t.Fatalf("hint = %#v, want waiting on /health", result[0]["hint"])
	}
	if result[0]["session_id"] != "session-ui-owner" {
		t.Fatalf("session_id = %#v, want session-ui-owner", result[0]["session_id"])
	}
}

func TestAPIStatus_RegressionMatrixPayload(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "matrix", Services: []Service{
				{
					Name:        "matrix-port-conflict",
					Command:     "python3 -m http.server 28180",
					HealthCheck: HealthCheck{URL: "http://127.0.0.1:28180/health"},
				},
				{
					Name:        "matrix-runtime-prereq-blocked",
					Command:     "docker compose up",
					HealthCheck: HealthCheck{URL: "http://127.0.0.1:28181/health"},
				},
				{
					Name:        "matrix-prereq-repaired-healthy",
					Command:     "docker compose up",
					HealthCheck: HealthCheck{URL: "http://127.0.0.1:28182/health"},
				},
				{
					Name:        "matrix-non-owner-rejected",
					Command:     "echo worker",
					HealthCheck: HealthCheck{URL: "http://127.0.0.1:28183/health"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	store.RecordPreflightFailure(
		"matrix-port-conflict",
		domain.ServiceFailureCodePortConflict,
		"PRECHECK_PORT_CONFLICT: foreign listener on 28180",
	)
	store.RecordPreflightFailure(
		"matrix-runtime-prereq-blocked",
		domain.ServiceFailureCodeRuntimePrereq,
		"PRECHECK_RUNTIME_PREREQ_FAILED: docker daemon unavailable",
	)
	store.Update("matrix-prereq-repaired-healthy", StatusHealthy, "")

	ownership, err := domain.NewServiceOwnership(
		"matrix-non-owner-rejected",
		"owner-session",
		4321,
		"config-hash",
		"http://127.0.0.1:28183/health",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := runner.ownershipRepo.Save(ownership); err != nil {
		t.Fatalf("Save ownership: %v", err)
	}
	store.Update("matrix-non-owner-rejected", StatusHealthy, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var payload []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(payload) != 4 {
		t.Fatalf("len = %d, want 4", len(payload))
	}

	byName := make(map[string]map[string]any, len(payload))
	for _, item := range payload {
		name, _ := item["name"].(string)
		byName[name] = item
	}
	for _, name := range []string{
		"matrix-port-conflict",
		"matrix-runtime-prereq-blocked",
		"matrix-prereq-repaired-healthy",
		"matrix-non-owner-rejected",
	} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("status payload missing service %q", name)
		}
	}

	if byName["matrix-port-conflict"]["failure_code"] != domain.ServiceFailureCodePortConflict {
		t.Fatalf("port conflict failure_code = %#v", byName["matrix-port-conflict"]["failure_code"])
	}
	if !strings.Contains(byName["matrix-port-conflict"]["hint"].(string), domain.ServiceFailureCodePortConflict) {
		t.Fatalf("port conflict hint should contain failure code, got %#v", byName["matrix-port-conflict"]["hint"])
	}

	if byName["matrix-runtime-prereq-blocked"]["failure_code"] != domain.ServiceFailureCodeRuntimePrereq {
		t.Fatalf("runtime prereq failure_code = %#v", byName["matrix-runtime-prereq-blocked"]["failure_code"])
	}
	if !strings.Contains(byName["matrix-runtime-prereq-blocked"]["hint"].(string), domain.ServiceFailureCodeRuntimePrereq) {
		t.Fatalf("runtime prereq hint should contain failure code, got %#v", byName["matrix-runtime-prereq-blocked"]["hint"])
	}

	if byName["matrix-prereq-repaired-healthy"]["status"] != string(StatusHealthy) {
		t.Fatalf("prereq repaired status = %#v, want healthy", byName["matrix-prereq-repaired-healthy"]["status"])
	}
	if gotCode := byName["matrix-prereq-repaired-healthy"]["failure_code"]; gotCode != nil && gotCode != "" {
		t.Fatalf("prereq repaired failure_code = %#v, want empty", gotCode)
	}

	if byName["matrix-non-owner-rejected"]["session_id"] != "owner-session" {
		t.Fatalf("non-owner case session_id = %#v, want owner-session", byName["matrix-non-owner-rejected"]["session_id"])
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
		`runAll Status`,
		`const dotClass = {`,
		`stopped: 'gray'`,
		`function fetchStatusData() {`,
		`function isServiceStartable(status) {`,
		`function getServiceStatus(name, data) {`,
		`const toggleAction = startable ? 'start' : 'stop';`,
		`data-action="stop-group"`,
		`data-action="build"`,
		`data-action="restart"`,
		`data-action="logs"`,
		`data-action="clear-logs"`,
		`id="logs-panel-grafana"`,
		`id="logs-panel-refresh"`,
		`id="logs-panel-copy"`,
		`function openGrafanaTraceLogs()`,
		`function extractTraceIdFromLogRows(rows)`,
		`/api/observability/grafana-trace`,
		`async function copyLogsToClipboard()`,
		`navigator.clipboard.writeText`,
		`id="pane-divider"`,
		`function openLogsPanel(name)`,
		`function closeLogsPanel()`,
		`function postGroupAction(url, group, label)`,
		`JSON.stringify({group: group, session_id: getSessionID()})`,
		`/api/build`,
		`/api/stop`,
		`/api/start`,
		`/api/stop-group`,
		`/api/logs`,
		`/api/logs/clear`,
		`function normalizePortValue(port)`,
		`function resolveHealthHref(url, fallbackPort)`,
		`function normalizePortHref(href)`,
		`target="_blank"`,
		`rel="noopener noreferrer"`,
		`health/command:`,
		`class="language"`,
		`svc.buildable === true`,
		`is-disabled`,
		`未配置 build_command，不可编译`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(body, snippet) {
			t.Fatalf("home page missing required snippet %q", snippet)
		}
	}
}

func TestUIHomePage_LogCopyFailureFallbackPresent(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"svc"})

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, nil)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, snippet := range []string{
		"function copyLogsToClipboard()",
		"updateLogsMeta('copy failed",
		"copied at",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("status.html missing log copy snippet %q", snippet)
		}
	}
}

func TestUIHomePage_PortLinkBoundaryGuardsPresent(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"svc"})

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, nil)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()

	requiredSnippets := []string{
		`function normalizePortValue(port)`,
		`function resolveHealthHref(url, fallbackPort)`,
		`function portLink(port, href)`,
		`return '-'`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(body, snippet) {
			t.Fatalf("status.html missing port boundary snippet %q", snippet)
		}
	}
}

func TestUIHomePage_PortLinkSchemeGuardsPresent(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"svc"})

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, nil)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()

	requiredSnippets := []string{
		`function normalizePortHref(href)`,
		`http:' && parsed.protocol !== 'https:'`,
		`new URL(`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(body, snippet) {
			t.Fatalf("status.html missing port href scheme guard snippet %q", snippet)
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

func TestAPIObservabilityGrafanaTrace_FromServiceLogs(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Observability: Observability{
			GrafanaURL:        "http://127.0.0.1:3000",
			TraceDashboardUID: "trace-log-journey",
		},
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "saas-backend",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9995"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	entry, err := domain.NewLogEntry(
		time.Now(),
		"saas-backend",
		domain.StreamStdout,
		`{"trace_id":"trace-api-test1234","msg":"relay accepted"}`,
	)
	if err != nil {
		t.Fatalf("NewLogEntry: %v", err)
	}
	runner.logRepository.Append("saas-backend", entry)

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodGet, "/api/observability/grafana-trace?service=saas-backend", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		TraceID string `json:"trace_id"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if response.TraceID != "trace-api-test1234" {
		t.Fatalf("trace_id = %q", response.TraceID)
	}
	if !strings.Contains(response.URL, "var-trace_id=trace-api-test1234") {
		t.Fatalf("url = %q", response.URL)
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

func TestUILogsPanel_CloseAndRefreshGuardsPresent(t *testing.T) {
	store := NewStatusStore()
	store.Init([]string{"svc"})

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, nil)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()

	requiredSnippets := []string{
		"function closeLogsPanel()",
		"function stopLogsAutoRefresh()",
		"function fetchLogsOnce()",
		"function clearLogs(name)",
		"function handleDividerPointerDown(event)",
		"function handleDividerPointerMove(event)",
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

func TestAPIStopService(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-stop",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9986"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("svc-stop", StatusHealthy, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/stop", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusMethodNotAllowed, "method not allowed")
	})

	t.Run("missing name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/stop", strings.NewReader(`{"name":"   "}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "name is required")
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/stop", strings.NewReader(`{"name":"svc-stop"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "session_id is required")
	})

	t.Run("runner nil", func(t *testing.T) {
		muxWithNilRunner := http.NewServeMux()
		registerUIHandlers(muxWithNilRunner, store, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/stop", strings.NewReader(`{"name":"svc-stop","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		muxWithNilRunner.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "runner is required")
	})

	t.Run("runner error passthrough", func(t *testing.T) {
		expectedErr := runner.StopServiceWithActor(context.Background(), "missing-service", "owner-session")
		if expectedErr == nil {
			t.Fatal("expected StopService error for missing service")
		}

		req := httptest.NewRequest(http.MethodPost, "/api/stop", strings.NewReader(`{"name":"missing-service","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, expectedErr.Error())
	})

	t.Run("non owner rejected when cascade false", func(t *testing.T) {
		store.SetPID("svc-stop", 1234)
		ownership, err := domain.NewServiceOwnership(
			"svc-stop",
			"owner-session",
			1234,
			"config-hash",
			"http://localhost:9986",
			time.Now(),
		)
		if err != nil {
			t.Fatalf("NewServiceOwnership: %v", err)
		}
		if err := runner.ownershipRepo.Save(ownership); err != nil {
			t.Fatalf("Save ownership: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/stop", strings.NewReader(`{"name":"svc-stop","session_id":"other-session","cascade":false}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertLifecycleAccepted(t, rec)
		waitForServiceStatus(t, store, "svc-stop", StatusHealthy, 2*time.Second)
	})

	t.Run("success", func(t *testing.T) {
		store.Update("svc-stop", StatusHealthy, "")

		req := httptest.NewRequest(http.MethodPost, "/api/stop", strings.NewReader(`{"name":"svc-stop","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertLifecycleAccepted(t, rec)
		waitForServiceStatus(t, store, "svc-stop", StatusStopped, 2*time.Second)
	})
}

func TestAPIStartService(t *testing.T) {
	store := NewStatusStore()
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{
					Name:        "svc-start",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: healthServer.URL},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	stubNoPortListenersForTest(runner)
	store.Update("svc-start", StatusStopped, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/start", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusMethodNotAllowed, "method not allowed")
	})

	t.Run("missing name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/start", strings.NewReader(`{"name":"   "}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "name is required")
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/start", strings.NewReader(`{"name":"svc-start"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "session_id is required")
	})

	t.Run("runner nil", func(t *testing.T) {
		muxWithNilRunner := http.NewServeMux()
		registerUIHandlers(muxWithNilRunner, store, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/start", strings.NewReader(`{"name":"svc-start","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		muxWithNilRunner.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "runner is required")
	})

	t.Run("runner error passthrough", func(t *testing.T) {
		expectedErr := runner.StartServiceWithActor(context.Background(), "missing-service", "owner-session")
		if expectedErr == nil {
			t.Fatal("expected StartService error for missing service")
		}

		req := httptest.NewRequest(http.MethodPost, "/api/start", strings.NewReader(`{"name":"missing-service","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, expectedErr.Error())
	})

	t.Run("success", func(t *testing.T) {
		store.Update("svc-start", StatusStopped, "")

		req := httptest.NewRequest(http.MethodPost, "/api/start", strings.NewReader(`{"name":"svc-start","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertLifecycleAccepted(t, rec)
		waitForServiceStatus(t, store, "svc-start", StatusHealthy, 5*time.Second)
	})

	t.Run("on_failure skip startup failure returns error", func(t *testing.T) {
		failStore := NewStatusStore()
		failRunner, err := NewRunner(&Config{
			Version: "1",
			Groups: []Group{
				{Name: "g-fail", Services: []Service{
					{
						Name:    "svc-start-skip-fail",
						Command: "sleep 1",
						HealthCheck: HealthCheck{
							URL:     "http://127.0.0.1:65534/unhealthy",
							Timeout: 1,
							Retries: 1,
							Backoff: Backoff{
								Initial:    0.1,
								Max:        0.1,
								Multiplier: 1.0,
							},
						},
						OnFailure: "skip",
					},
				}},
			},
		}, failStore)
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}
		stubNoPortListenersForTest(failRunner)
		failStore.Update("svc-start-skip-fail", StatusStopped, "")

		failMux := http.NewServeMux()
		registerUIHandlers(failMux, failStore, failRunner)

		req := httptest.NewRequest(http.MethodPost, "/api/start", strings.NewReader(`{"name":"svc-start-skip-fail","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		failMux.ServeHTTP(rec, req)
		assertLifecycleAccepted(t, rec)
		waitForServiceStatus(t, failStore, "svc-start-skip-fail", StatusFailed, 5*time.Second)
	})
}

func TestAPIRestart_OnFailureSkipStartupFailureReturnsError(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g-restart-fail", Services: []Service{
				{
					Name:    "svc-restart-skip-fail",
					Command: "sleep 1",
					HealthCheck: HealthCheck{
						URL:     "http://127.0.0.1:65534/unhealthy",
						Timeout: 1,
						Retries: 1,
						Backoff: Backoff{
							Initial:    0.1,
							Max:        0.1,
							Multiplier: 1.0,
						},
					},
					OnFailure: "skip",
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("svc-restart-skip-fail", StatusHealthy, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/restart", strings.NewReader(`{"name":"svc-restart-skip-fail","session_id":"owner-session"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertLifecycleAccepted(t, rec)
	waitForServiceStatus(t, store, "svc-restart-skip-fail", StatusFailed, 5*time.Second)
}

func TestAPIRestart_RequiresSessionID(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g-restart", Services: []Service{
				{
					Name:        "svc-restart",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9979"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("svc-restart", StatusHealthy, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/restart", strings.NewReader(`{"name":"svc-restart"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertJSONErrorMessage(t, rec, http.StatusBadRequest, "session_id is required")
}

func TestAPIRestart_NonOwnerRejected(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g-restart", Services: []Service{
				{
					Name:        "svc-restart-owned",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9978"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("svc-restart-owned", StatusHealthy, "")
	ownership, err := domain.NewServiceOwnership(
		"svc-restart-owned",
		"owner-session",
		1234,
		"config-hash",
		"http://localhost:9978",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := runner.ownershipRepo.Save(ownership); err != nil {
		t.Fatalf("Save ownership: %v", err)
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/restart", strings.NewReader(`{"name":"svc-restart-owned","session_id":"other-session"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertLifecycleAccepted(t, rec)
	waitForServiceStatus(t, store, "svc-restart-owned", StatusHealthy, 2*time.Second)
}

func TestAPIStopGroup(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g-stop", Services: []Service{
				{
					Name:        "svc-stop-group",
					Command:     "echo running",
					HealthCheck: HealthCheck{URL: "http://localhost:9988"},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("svc-stop-group", StatusHealthy, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/stop-group", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusMethodNotAllowed, "method not allowed")
	})

	t.Run("missing group", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/stop-group", strings.NewReader(`{"group":"   "}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "group is required")
	})

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/stop-group", strings.NewReader(`{"group":"g-stop"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "session_id is required")
	})

	t.Run("runner nil", func(t *testing.T) {
		muxWithNilRunner := http.NewServeMux()
		registerUIHandlers(muxWithNilRunner, store, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/stop-group", strings.NewReader(`{"group":"g-stop","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		muxWithNilRunner.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, "runner is required")
	})

	t.Run("runner error passthrough", func(t *testing.T) {
		expectedErr := runner.StopGroup(context.Background(), "missing-group")
		if expectedErr == nil {
			t.Fatal("expected StopGroup error for missing group")
		}

		req := httptest.NewRequest(http.MethodPost, "/api/stop-group", strings.NewReader(`{"group":"missing-group","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertJSONErrorMessage(t, rec, http.StatusBadRequest, expectedErr.Error())
	})

	t.Run("non owner delegates to registered owner", func(t *testing.T) {
		store.Update("svc-stop-group", StatusHealthy, "")
		store.SetPID("svc-stop-group", 1234)
		ownership, err := domain.NewServiceOwnership(
			"svc-stop-group",
			"owner-session",
			1234,
			"config-hash",
			"http://localhost:9988",
			time.Now(),
		)
		if err != nil {
			t.Fatalf("NewServiceOwnership: %v", err)
		}
		if err := runner.ownershipRepo.Save(ownership); err != nil {
			t.Fatalf("Save ownership: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/stop-group", strings.NewReader(`{"group":"g-stop","session_id":"other-session"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertLifecycleAccepted(t, rec)
		waitForServiceStatus(t, store, "svc-stop-group", StatusStopped, 2*time.Second)
	})

	t.Run("success", func(t *testing.T) {
		store.Update("svc-stop-group", StatusHealthy, "")

		req := httptest.NewRequest(http.MethodPost, "/api/stop-group", strings.NewReader(`{"group":"g-stop","session_id":"owner-session"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assertLifecycleAccepted(t, rec)
		waitForServiceStatus(t, store, "svc-stop-group", StatusStopped, 2*time.Second)
	})
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
	var body map[string]any
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	errMsg, _ := body["error"].(string)
	if strings.TrimSpace(errMsg) == "" {
		t.Fatalf("error body = %#v, want non-empty error", body)
	}
}

func assertJSONErrorMessage(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMessage string) {
	t.Helper()
	assertJSONErrorResponse(t, rec, wantStatus)
	var body map[string]any
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	errMsg, _ := body["error"].(string)
	if errMsg != wantMessage {
		t.Fatalf("error = %q, want %q", errMsg, wantMessage)
	}
}

func TestReadCascadeFlag(t *testing.T) {
	if !readCascadeFlag(map[string]any{}) {
		t.Fatal("expected default cascade true when field missing")
	}
	if readCascadeFlag(map[string]any{"cascade": false}) {
		t.Fatal("expected cascade false")
	}
	if !readCascadeFlag(map[string]any{"cascade": true}) {
		t.Fatal("expected cascade true")
	}
}

func TestAPIStopService_CascadeFalseBlocksDownstream(t *testing.T) {
	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g1", Services: []Service{
				{Name: "a", Command: "echo a", HealthCheck: HealthCheck{URL: "http://127.0.0.1:1"}},
				{Name: "b", Command: "echo b", DependsOn: []string{"a"}, HealthCheck: HealthCheck{URL: "http://127.0.0.1:1"}},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store.Update("a", StatusHealthy, "")
	store.Update("b", StatusHealthy, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/stop", strings.NewReader(`{"name":"a","session_id":"owner-session","cascade":false}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertLifecycleAccepted(t, rec)
	waitForServiceStatus(t, store, "a", StatusHealthy, 2*time.Second)
}

func TestAPIStartGroup(t *testing.T) {
	store := NewStatusStore()
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{Name: "g-start", Services: []Service{
				{
					Name:        "svc-group-start",
					Command:     "sleep 30",
					HealthCheck: HealthCheck{URL: healthServer.URL, Timeout: 2, Retries: 2, CheckInterval: 1, Backoff: Backoff{Initial: 0.1, Max: 0.2, Multiplier: 1.5}},
				},
			}},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	stubNoPortListenersForTest(runner)
	store.Update("svc-group-start", StatusStopped, "")

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/start-group", strings.NewReader(`{"group":"g-start","session_id":"owner-session"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertLifecycleAccepted(t, rec)
	waitForServiceStatus(t, store, "svc-group-start", StatusHealthy, 5*time.Second)
}
