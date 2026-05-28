package domain

import (
	"strings"
	"testing"
)

func TestGrafanaTraceLink(t *testing.T) {
	link, err := GrafanaTraceLink(
		"http://127.0.0.1:3000",
		"trace-log-journey",
		"trace-link-test1234",
	)
	if err != nil {
		t.Fatalf("GrafanaTraceLink: %v", err)
	}
	for _, part := range []string{
		"127.0.0.1:3000",
		"/d/trace-log-journey/",
		"var-trace_id=trace-link-test1234",
	} {
		if !strings.Contains(link, part) {
			t.Fatalf("link %q missing %q", link, part)
		}
	}
}
