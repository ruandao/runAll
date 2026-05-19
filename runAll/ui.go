package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
)

//go:embed status.html
var statusHTML embed.FS

func registerUIHandlers(mux *http.ServeMux, store *StatusStore) {
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(store.All()); err != nil {
			log.Printf("[ui] json encode error: %v", err)
		}
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

func startUIServer(store *StatusStore, port string) *http.Server {
	mux := http.NewServeMux()
	registerUIHandlers(mux, store)

	srv := &http.Server{Addr: port, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[ui] server error: %v", err)
		}
	}()
	return srv
}
