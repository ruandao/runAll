package domain

import (
	"testing"
	"time"
)

func TestNewLogEntry_RejectsWhitespaceServiceName(t *testing.T) {
	_, err := NewLogEntry(time.Now(), "   ", StreamStdout, "hello")
	if err == nil {
		t.Fatal("expected error for whitespace service name")
	}
}

func TestNewLogEntry_ValidationAndEqualityBoundaries(t *testing.T) {
	t.Run("zero timestamp defaults to now", func(t *testing.T) {
		entry, err := NewLogEntry(time.Time{}, "svc-a", StreamStdout, "hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entry.Timestamp.IsZero() {
			t.Fatal("timestamp should be auto-filled when zero")
		}
	})

	t.Run("same inputs produce equal value objects", func(t *testing.T) {
		ts := time.Unix(1710000000, 0).UTC()
		left, err := NewLogEntry(ts, "svc-a", StreamStderr, "line-1")
		if err != nil {
			t.Fatalf("left create error: %v", err)
		}
		right, err := NewLogEntry(ts, "svc-a", StreamStderr, "line-1")
		if err != nil {
			t.Fatalf("right create error: %v", err)
		}
		if left != right {
			t.Fatalf("expected value equality, left=%#v right=%#v", left, right)
		}
	})
}
