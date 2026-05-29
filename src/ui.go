package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"runAll/src/domain"
)

//go:embed status.html
var statusHTML embed.FS

const maxLogsLines = 2000

type serviceStatusPayload struct {
	*ServiceStatus
	Hint      string `json:"hint,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Buildable bool   `json:"buildable"`
	Language  string `json:"language"`
}

func registerUIHandlers(mux *http.ServeMux, store *StatusStore, runner *Runner) {
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if runner != nil {
			runner.reconcileFailedServiceHealth(r.Context())
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(buildStatusPayload(store, runner)); err != nil {
			log.Printf("[ui] json encode error: %v", err)
		}
	})

	mux.HandleFunc("/api/restart", func(w http.ResponseWriter, r *http.Request) {
		handleServiceActionWithSession(w, r, runner, "name", func(ctx context.Context, name string, sessionID string) error {
			log.Printf("[api] restart request for %s", name)
			return runner.RestartServiceWithActor(ctx, name, sessionID)
		})
	})

	mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		handleCascadeServiceActionWithSession(
			w, r, runner, "name",
			runner.StopServiceCascadeWithActor,
			runner.StopServiceWithActor,
		)
	})

	mux.HandleFunc("/api/start", func(w http.ResponseWriter, r *http.Request) {
		handleCascadeServiceActionWithSession(
			w, r, runner, "name",
			runner.StartServiceCascadeWithActor,
			runner.StartServiceWithActor,
		)
	})

	mux.HandleFunc("/api/stop-group", func(w http.ResponseWriter, r *http.Request) {
		handleServiceActionWithSession(w, r, runner, "group", runner.StopGroupWithActor)
	})

	mux.HandleFunc("/api/start-group", func(w http.ResponseWriter, r *http.Request) {
		handleServiceActionWithSession(w, r, runner, "group", runner.StartGroupWithActor)
	})

	mux.HandleFunc("/api/build", func(w http.ResponseWriter, r *http.Request) {
		handleServiceAction(w, r, runner, "name", func(ctx context.Context, name string) error {
			log.Printf("[api] build request for %s", name)
			return runner.BuildService(ctx, name)
		})
	})

	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if runner == nil {
			writeJSONError(w, "runner is required")
			return
		}

		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			writeJSONError(w, "name is required")
			return
		}
		if runner.findService(name) == nil {
			writeJSONError(w, fmt.Sprintf("service %q not found", name))
			return
		}

		linesRaw := r.URL.Query().Get("lines")
		if linesRaw == "" {
			writeJSONError(w, "lines is required")
			return
		}
		lines, err := strconv.Atoi(linesRaw)
		if err != nil || lines <= 0 {
			writeJSONError(w, "lines must be a positive integer")
			return
		}
		if lines > maxLogsLines {
			writeJSONError(w, fmt.Sprintf("lines must be <= %d", maxLogsLines))
			return
		}
		if runner.logRepository == nil {
			writeJSONError(w, "log repository is required")
			return
		}

		writeJSON(w, map[string]any{
			"name":  name,
			"lines": runner.logRepository.Tail(name, lines),
		})
	})

	mux.HandleFunc("/api/logs/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, "invalid json")
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			writeJSONError(w, "name is required")
			return
		}
		if runner == nil {
			writeJSONError(w, "runner is required")
			return
		}
		if runner.logRepository == nil {
			writeJSONError(w, "log repository is required")
			return
		}
		if runner.findService(body.Name) == nil {
			writeJSONError(w, fmt.Sprintf("service %q not found", body.Name))
			return
		}

		runner.logRepository.Clear(body.Name)
		writeJSON(w, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/observability", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if runner == nil || runner.cfg == nil {
			writeJSONError(w, "runner is required")
			return
		}
		obs := runner.cfg.Observability
		lokiExplore, err := domain.GrafanaLokiExploreLink(obs.GrafanaURL)
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		tempoExplore, err := domain.GrafanaTempoExploreLink(obs.GrafanaURL, "")
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		payload := map[string]string{
			"grafana_url":           obs.GrafanaURL,
			"loki_url":              obs.LokiURL,
			"grafana_loki_explore":  lokiExplore,
			"grafana_tempo_explore": tempoExplore,
			"trace_dashboard_uid":   obs.TraceDashboardUID,
			"log_file_root":         strings.TrimSpace(runner.cfg.Logging.FileRoot),
		}
		writeJSON(w, payload)
	})

	mux.HandleFunc("/api/observability/grafana-trace", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if runner == nil || runner.cfg == nil {
			writeJSONError(w, "runner is required")
			return
		}

		traceID := strings.TrimSpace(r.URL.Query().Get("trace_id"))
		serviceName := strings.TrimSpace(r.URL.Query().Get("service"))
		if traceID == "" && serviceName != "" && runner.logRepository != nil {
			entries := runner.logRepository.Tail(serviceName, 200)
			for i := len(entries) - 1; i >= 0; i-- {
				if tid := domain.ExtractTraceIDFromLogMessage(entries[i].Message); tid != "" {
					traceID = tid
					break
				}
			}
		}
		if traceID == "" {
			writeJSONError(w, "trace_id is required (or provide service with trace logs)")
			return
		}

		link, err := domain.GrafanaTraceLink(
			runner.cfg.Observability.GrafanaURL,
			runner.cfg.Observability.TraceDashboardUID,
			traceID,
		)
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		writeJSON(w, map[string]string{
			"trace_id": traceID,
			"url":      link,
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := statusHTML.ReadFile("status.html")
		if err != nil {
			log.Printf("[ui] read embedded html error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[ui] json encode error: %v", err)
	}
}

func buildStatusPayload(store *StatusStore, runner *Runner) []serviceStatusPayload {
	services := store.All()
	result := make([]serviceStatusPayload, 0, len(services))
	for _, svc := range services {
		payload := serviceStatusPayload{
			ServiceStatus: svc,
			Hint:          deriveFailureHint(svc),
			Buildable:     false,
			Language:      "—",
		}
		if runner != nil {
			payload.SessionID = resolveServiceSessionID(runner, svc.Name)
			if cfgSvc := runner.findService(svc.Name); cfgSvc != nil {
				payload.Buildable = serviceBuildable(cfgSvc)
				payload.Language = detectServiceLanguage(*cfgSvc)
			}
		}
		result = append(result, payload)
	}
	return result
}

func writeJSONError(w http.ResponseWriter, message string) {
	writeJSONErrorWithStatus(w, http.StatusBadRequest, message)
}

func writeJSONErrorWithStatus(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		log.Printf("[ui] json encode error: %v", err)
	}
}

func handleServiceAction(w http.ResponseWriter, r *http.Request, runner *Runner, fieldName string, action func(context.Context, string) error) {
	if r.Method != http.MethodPost {
		writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	body, err := readStringFieldFromJSON(r, fieldName)
	if err != nil {
		writeJSONError(w, err.Error())
		return
	}
	if runner == nil {
		writeJSONError(w, "runner is required")
		return
	}
	if err := action(r.Context(), body); err != nil {
		writeJSONError(w, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func handleServiceActionWithSession(
	w http.ResponseWriter,
	r *http.Request,
	runner *Runner,
	fieldName string,
	action func(context.Context, string, string) error,
) {
	if r.Method != http.MethodPost {
		writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONError(w, err.Error())
		return
	}
	body, err := readRequiredStringField(payload, fieldName)
	if err != nil {
		writeJSONError(w, err.Error())
		return
	}
	sessionID, err := readRequiredStringField(payload, "session_id")
	if err != nil {
		writeJSONError(w, err.Error())
		return
	}
	if runner == nil {
		writeJSONError(w, "runner is required")
		return
	}
	if err := validateLifecycleTarget(runner, fieldName, body); err != nil {
		writeJSONError(w, err.Error())
		return
	}
	runLifecycleActionAsync(func(ctx context.Context) error {
		return action(ctx, body, sessionID)
	}, fieldName, body)
	writeLifecycleAccepted(w)
}

func handleCascadeServiceActionWithSession(
	w http.ResponseWriter,
	r *http.Request,
	runner *Runner,
	fieldName string,
	cascadeAction func(context.Context, string, string) error,
	singleAction func(context.Context, string, string) error,
) {
	if r.Method != http.MethodPost {
		writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONError(w, err.Error())
		return
	}
	body, err := readRequiredStringField(payload, fieldName)
	if err != nil {
		writeJSONError(w, err.Error())
		return
	}
	sessionID, err := readRequiredStringField(payload, "session_id")
	if err != nil {
		writeJSONError(w, err.Error())
		return
	}
	if runner == nil {
		writeJSONError(w, "runner is required")
		return
	}
	if err := validateLifecycleTarget(runner, fieldName, body); err != nil {
		writeJSONError(w, err.Error())
		return
	}
	action := cascadeAction
	if !readCascadeFlag(payload) {
		action = singleAction
	}
	runLifecycleActionAsync(func(ctx context.Context) error {
		return action(ctx, body, sessionID)
	}, fieldName, body)
	writeLifecycleAccepted(w)
}

func validateLifecycleTarget(runner *Runner, fieldName, body string) error {
	switch fieldName {
	case "name":
		if runner.findService(body) == nil {
			return fmt.Errorf("service %q not found", body)
		}
	case "group":
		for _, group := range runner.cfg.Groups {
			if group.Name == body {
				return nil
			}
		}
		return fmt.Errorf("group %q not found", body)
	default:
		return fmt.Errorf("unsupported lifecycle target %q", fieldName)
	}
	return nil
}

func runLifecycleActionAsync(action func(context.Context) error, fieldName, body string) {
	go func() {
		if err := action(context.Background()); err != nil {
			log.Printf("[ui] async %s %q failed: %v", fieldName, body, err)
		}
	}()
}

func writeLifecycleAccepted(w http.ResponseWriter) {
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]string{"status": "accepted"})
}

func readCascadeFlag(payload map[string]any) bool {
	raw, ok := payload["cascade"]
	if !ok {
		return true
	}
	value, ok := raw.(bool)
	if !ok {
		return true
	}
	return value
}

func writeCascadeError(w http.ResponseWriter, failure *CascadeFailure) {
	message := "cascade failed"
	if failure != nil && failure.Err != nil {
		message = failure.Err.Error()
	}
	payload := map[string]any{"error": message}
	if failure != nil {
		payload["cascade"] = map[string]any{
			"completed": failure.Report.Completed,
			"failed_at": failure.Report.FailedAt,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[ui] json encode error: %v", err)
	}
}

func readStringFieldFromJSON(r *http.Request, fieldName string) (string, error) {
	payload, err := readJSONPayload(r)
	if err != nil {
		return "", err
	}
	return readRequiredStringField(payload, fieldName)
}

func readJSONPayload(r *http.Request) (map[string]any, error) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("invalid json")
	}
	return payload, nil
}

func readRequiredStringField(payload map[string]any, fieldName string) (string, error) {
	raw, ok := payload[fieldName]
	if !ok {
		return "", fmt.Errorf("%s is required", fieldName)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s is required", fieldName)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", fieldName)
	}
	return value, nil
}

func startUIServer(store *StatusStore, runner *Runner, port string) *http.Server {
	mux := http.NewServeMux()
	registerUIHandlers(mux, store, runner)

	srv := &http.Server{Addr: port, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[ui] server error: %v", err)
		}
	}()
	return srv
}
