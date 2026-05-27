package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"runAll/src/domain"
)

func TestPlaywrightUIServer(t *testing.T) {
	if os.Getenv("RUNALL_PLAYWRIGHT_SERVER") != "1" {
		t.Skip("set RUNALL_PLAYWRIGHT_SERVER=1 to start fixture server")
	}

	store := NewStatusStore()
	runner, err := NewRunner(&Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "platform",
				Services: []Service{
					{
						Name:        "git-oauth",
						Command:     "echo git-oauth",
						HealthCheck: HealthCheck{URL: "http://127.0.0.1:8002/api/health/", Timeout: 1, Retries: 1},
					},
					{
						Name:        "saas-backend",
						Command:     "echo saas-backend",
						DependsOn:   []string{"git-oauth"},
						HealthCheck: HealthCheck{URL: "http://127.0.0.1:8001/api/health/", Timeout: 1, Retries: 1},
					},
					{
						Name:        "vue-frontend",
						Command:     "sleep 120",
						DependsOn:   []string{"saas-backend"},
						HealthCheck: HealthCheck{URL: "http://127.0.0.1:4000/health", Timeout: 1, Retries: 1},
					},
					{
						Name:        "ai-provider",
						Command:     "echo ai-provider",
						DependsOn:   []string{"saas-backend"},
						HealthCheck: HealthCheck{URL: "http://127.0.0.1:8010/api/health/", Timeout: 1, Retries: 1},
					},
				},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	for _, name := range []string{"git-oauth", "saas-backend", "vue-frontend", "ai-provider"} {
		store.Update(name, StatusHealthy, "")
	}

	saveOwnership := func(serviceName, ownerSession string) {
		t.Helper()
		ownership, err := domain.NewServiceOwnership(
			serviceName,
			ownerSession,
			1234,
			"config-hash",
			"http://127.0.0.1:1",
			time.Now(),
		)
		if err != nil {
			t.Fatalf("NewServiceOwnership(%s): %v", serviceName, err)
		}
		if err := runner.ownershipRepo.Save(ownership); err != nil {
			t.Fatalf("Save ownership(%s): %v", serviceName, err)
		}
	}
	saveOwnership("vue-frontend", defaultOwnershipSessionID)
	saveOwnership("saas-backend", defaultOwnershipSessionID)
	saveOwnership("ai-provider", defaultOwnershipSessionID)
	saveOwnership("git-oauth", "playwright-ui-session")

	resetPlaywrightFixture := func() {
		for _, name := range []string{"git-oauth", "saas-backend", "vue-frontend", "ai-provider"} {
			store.Update(name, StatusHealthy, "")
			store.SetPID(name, 0)
		}
		saveOwnership("vue-frontend", defaultOwnershipSessionID)
		saveOwnership("saas-backend", defaultOwnershipSessionID)
		saveOwnership("ai-provider", defaultOwnershipSessionID)
		saveOwnership("git-oauth", "playwright-ui-session")
	}

	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)
	mux.HandleFunc("/api/test/reset-fixture", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		resetPlaywrightFixture()
		writeJSON(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/test/set-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, "invalid json")
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		body.Status = strings.TrimSpace(body.Status)
		if body.Name == "" || body.Status == "" {
			writeJSONError(w, "name and status are required")
			return
		}
		store.Update(body.Name, Status(body.Status), "")
		store.SetPID(body.Name, 0)
		writeJSON(w, map[string]string{"status": "ok"})
	})

	ln, err := net.Listen("tcp", "127.0.0.1:19999")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Logf("Playwright UI server listening on http://%s", ln.Addr().String())
	if err := http.Serve(ln, mux); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}
