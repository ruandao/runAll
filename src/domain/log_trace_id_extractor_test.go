package domain

import "testing"

func TestExtractTraceIDFromLogMessage_JSON(t *testing.T) {
	line := `{"ts":"2026-05-28T10:00:00Z","level":"info","service":"saas-backend","trace_id":"trace-json-test1234","msg":"hello"}`
	got := ExtractTraceIDFromLogMessage(line)
	if got != "trace-json-test1234" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractTraceIDFromLogMessage_Bracket(t *testing.T) {
	line := `2026-05-28 10:00:00 - cloud - INFO - [trace_id=trace-bracket123456] - started`
	got := ExtractTraceIDFromLogMessage(line)
	if got != "trace-bracket123456" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractTraceIDFromLogMessage_Empty(t *testing.T) {
	if got := ExtractTraceIDFromLogMessage("no trace here"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractTraceIDFromLogMessage_TraceIdCamelCase(t *testing.T) {
	line := `{"ts":"2026-05-28T10:00:00Z","level":"info","service":"onlineServiceJS","traceId":"trace-camel-test1234","msg":"hello"}`
	got := ExtractTraceIDFromLogMessage(line)
	if got != "trace-camel-test1234" {
		t.Fatalf("got %q", got)
	}
}
