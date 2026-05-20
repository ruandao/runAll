package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

//go:embed status.html
var statusHTML embed.FS

const maxLogsLines = 2000

func registerUIHandlers(mux *http.ServeMux, store *StatusStore, runner *Runner) {
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(store.All()); err != nil {
			log.Printf("[ui] json encode error: %v", err)
		}
	})

	mux.HandleFunc("/api/restart", func(w http.ResponseWriter, r *http.Request) {
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

		log.Printf("[api] restart request for %s", body.Name)
		if err := runner.RestartService(r.Context(), body.Name); err != nil {
			writeJSONError(w, err.Error())
			return
		}

		writeJSON(w, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/build", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONErrorWithStatus(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if runner == nil {
			writeJSONError(w, "runner is required")
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

		log.Printf("[api] build request for %s", body.Name)
		if err := runner.BuildService(r.Context(), body.Name); err != nil {
			writeJSONError(w, err.Error())
			return
		}

		writeJSON(w, map[string]string{"status": "ok"})
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
