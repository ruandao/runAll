package main

import (
	"embed"
	"encoding/json"
	"net/http"
)

//go:embed status.html
var statusHTML embed.FS

func registerUIHandlers(mux *http.ServeMux, store *StatusStore) {
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(store.All())
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, _ := statusHTML.ReadFile("status.html")
		w.Write(data)
	})
}

func startUIServer(store *StatusStore, port string) *http.Server {
	mux := http.NewServeMux()
	registerUIHandlers(mux, store)

	srv := &http.Server{Addr: port, Handler: mux}
	go srv.ListenAndServe()
	return srv
}
