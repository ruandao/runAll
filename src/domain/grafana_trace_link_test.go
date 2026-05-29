package domain

import (
	"strings"
	"testing"
)

func TestGrafanaTraceLink(t *testing.T) {
	link, err := GrafanaTraceLink(
		"http://127.0.0.1:3000",
		"distributed-trace-view",
		"trace-link-test1234",
	)
	if err != nil {
		t.Fatalf("GrafanaTraceLink: %v", err)
	}
	for _, part := range []string{
		"127.0.0.1:3000",
		"/d/distributed-trace-view",
		"var-trace_id=trace-link-test1234",
		"var-tempo_trace_id=",
	} {
		if !strings.Contains(link, part) && part != "var-tempo_trace_id=" {
			t.Fatalf("link %q missing %q", link, part)
		}
	}
	if !strings.Contains(link, "var-tempo_trace_id=") {
		t.Fatalf("link %q missing tempo_trace_id param", link)
	}
}

func TestGrafanaTempoExploreLink(t *testing.T) {
	link, err := GrafanaTempoExploreLink("http://127.0.0.1:3000", "trace-tempo-test1234")
	if err != nil {
		t.Fatalf("GrafanaTempoExploreLink: %v", err)
	}
	for _, part := range []string{
		"/explore",
		"tempo",
		"traceId",
		OtelTraceIDHex("trace-tempo-test1234"),
	} {
		if !strings.Contains(link, part) {
			t.Fatalf("link %q missing %q", link, part)
		}
	}
}

func TestGrafanaLokiExploreLink(t *testing.T) {
	link, err := GrafanaLokiExploreLink("http://127.0.0.1:3000")
	if err != nil {
		t.Fatalf("GrafanaLokiExploreLink: %v", err)
	}
	for _, part := range []string{
		"/explore",
		"loki",
		"runall",
	} {
		if !strings.Contains(link, part) {
			t.Fatalf("link %q missing %q", link, part)
		}
	}
}
