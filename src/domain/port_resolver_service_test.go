package domain

import "testing"

func TestResolveHealthPort(t *testing.T) {
	t.Run("returns explicit port when present", func(t *testing.T) {
		got := ResolveHealthPort("http://localhost:8080/health")
		if got != "8080" {
			t.Fatalf("expected 8080, got %q", got)
		}
	})

	t.Run("defaults to 80 for http without explicit port", func(t *testing.T) {
		got := ResolveHealthPort("http://service.internal/health")
		if got != "80" {
			t.Fatalf("expected 80, got %q", got)
		}
	})

	t.Run("defaults to 443 for https without explicit port", func(t *testing.T) {
		got := ResolveHealthPort("https://service.internal/status")
		if got != "443" {
			t.Fatalf("expected 443, got %q", got)
		}
	})

	t.Run("returns empty for invalid url", func(t *testing.T) {
		got := ResolveHealthPort("://invalid")
		if got != "" {
			t.Fatalf("expected empty port for invalid url, got %q", got)
		}
	})
}

func TestResolveCommandPort(t *testing.T) {
	t.Run("matches --port value form", func(t *testing.T) {
		got := ResolveCommandPort("npm run dev --port 3000")
		if got != "3000" {
			t.Fatalf("expected 3000, got %q", got)
		}
	})

	t.Run("matches --port equals form", func(t *testing.T) {
		got := ResolveCommandPort("vite --port=5173")
		if got != "5173" {
			t.Fatalf("expected 5173, got %q", got)
		}
	})

	t.Run("matches -p form", func(t *testing.T) {
		got := ResolveCommandPort("next dev -p 4000")
		if got != "4000" {
			t.Fatalf("expected 4000, got %q", got)
		}
	})

	t.Run("matches env assignment form", func(t *testing.T) {
		got := ResolveCommandPort("PORT=4200 npm run start")
		if got != "4200" {
			t.Fatalf("expected 4200, got %q", got)
		}
	})

	t.Run("matches host/addr with colon port", func(t *testing.T) {
		got := ResolveCommandPort("uvicorn app:app --host 0.0.0.0:9000")
		if got != "9000" {
			t.Fatalf("expected 9000, got %q", got)
		}
	})

	t.Run("matches ipv6 host token with bracketed port", func(t *testing.T) {
		got := ResolveCommandPort("server --host [::1]:8080")
		if got != "8080" {
			t.Fatalf("expected 8080, got %q", got)
		}
	})

	t.Run("returns first valid match by priority", func(t *testing.T) {
		got := ResolveCommandPort("PORT=7000 server --port 5000 --host 127.0.0.1:9000")
		if got != "5000" {
			t.Fatalf("expected 5000, got %q", got)
		}
	})

	t.Run("returns empty for no match", func(t *testing.T) {
		got := ResolveCommandPort("npm run build")
		if got != "" {
			t.Fatalf("expected empty for no match, got %q", got)
		}
	})

	t.Run("returns empty for out of range ports", func(t *testing.T) {
		tests := []string{
			"server --port 0",
			"server --port 65536",
			"PORT=70000 node app.js",
			"server --host 127.0.0.1:99999",
		}
		for _, tc := range tests {
			if got := ResolveCommandPort(tc); got != "" {
				t.Fatalf("expected empty for %q, got %q", tc, got)
			}
		}
	})

	t.Run("does not accept numeric truncation suffixes", func(t *testing.T) {
		tests := []string{
			"vite --port=3000abc",
			"next dev -p 8080foo",
			"PORT=5173x npm run dev",
			"PORT=3000x npm run dev",
		}
		for _, tc := range tests {
			if got := ResolveCommandPort(tc); got != "" {
				t.Fatalf("expected empty for %q, got %q", tc, got)
			}
		}
	})

	t.Run("does not treat generic colon tokens as host port", func(t *testing.T) {
		tests := []string{
			"docker run node:18",
			"docker run redis:7",
			"server --host ::1",
			"echo 12:34",
		}
		for _, tc := range tests {
			if got := ResolveCommandPort(tc); got != "" {
				t.Fatalf("expected empty for %q, got %q", tc, got)
			}
		}
	})
}
