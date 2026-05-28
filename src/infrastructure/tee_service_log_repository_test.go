package infrastructure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"runAll/src/domain"
)

func TestTeeServiceLogRepository_WritesMemoryAndFile(t *testing.T) {
	root := t.TempDir()
	memory := NewInMemoryServiceLogRepository(10)
	fileSink, err := NewFileServiceLogSink(root)
	if err != nil {
		t.Fatalf("NewFileServiceLogSink: %v", err)
	}
	t.Cleanup(func() { _ = fileSink.Close() })

	repo := NewTeeServiceLogRepository(memory, fileSink)
	entry, err := domain.NewLogEntry(time.Now(), "saas-backend", domain.StreamStdout, "line with [trace_id=abc12345678]")
	if err != nil {
		t.Fatalf("NewLogEntry: %v", err)
	}
	repo.Append("saas-backend", entry)

	tail := repo.Tail("saas-backend", 10)
	if len(tail) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(tail))
	}

	data, err := os.ReadFile(filepath.Join(root, "saas-backend.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "[trace_id=abc12345678]") {
		t.Fatalf("file missing trace line: %q", string(data))
	}
}

func TestTeeServiceLogRepository_FileFailureDoesNotPanic(t *testing.T) {
	memory := NewInMemoryServiceLogRepository(10)
	repo := NewTeeServiceLogRepository(memory, brokenFileSink{})

	entry, err := domain.NewLogEntry(time.Now(), "svc", domain.StreamStdout, "msg")
	if err != nil {
		t.Fatalf("NewLogEntry: %v", err)
	}
	repo.Append("svc", entry)

	if len(repo.Tail("svc", 10)) != 1 {
		t.Fatal("memory append should still work when file sink fails")
	}
}

type brokenFileSink struct{}

func (brokenFileSink) AppendLine(string, string, string) error {
	return os.ErrPermission
}

func (brokenFileSink) Close() error { return nil }
