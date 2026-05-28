package domain

import "testing"

func TestParseTraceId_Valid(t *testing.T) {
	tid, err := ParseTraceId("test-trace-abc12345")
	if err != nil {
		t.Fatalf("ParseTraceId: %v", err)
	}
	if tid.String() != "test-trace-abc12345" {
		t.Fatalf("got %q", tid.String())
	}
}

func TestParseTraceId_TooShort(t *testing.T) {
	_, err := ParseTraceId("short")
	if err == nil {
		t.Fatal("expected error for short trace id")
	}
}

func TestParseTraceId_Empty(t *testing.T) {
	_, err := ParseTraceId("   ")
	if err == nil {
		t.Fatal("expected error for empty trace id")
	}
}

func TestParseTraceId_InvalidChars(t *testing.T) {
	_, err := ParseTraceId("invalid trace id!!")
	if err == nil {
		t.Fatal("expected error for invalid chars")
	}
}
