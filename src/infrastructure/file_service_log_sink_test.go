package infrastructure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileServiceLogSink_AppendLine(t *testing.T) {
	root := t.TempDir()
	sink, err := NewFileServiceLogSink(root)
	if err != nil {
		t.Fatalf("NewFileServiceLogSink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	if err := sink.AppendLine("saas-backend", "stdout", "hello trace"); err != nil {
		t.Fatalf("AppendLine: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "saas-backend.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "(stdout) hello trace") {
		t.Fatalf("unexpected log content: %q", content)
	}
}

func TestFileServiceLogSink_RequiresRoot(t *testing.T) {
	_, err := NewFileServiceLogSink("")
	if err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestFileServiceLogSink_ServiceIsolation(t *testing.T) {
	root := t.TempDir()
	sink, err := NewFileServiceLogSink(root)
	if err != nil {
		t.Fatalf("NewFileServiceLogSink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	_ = sink.AppendLine("task-auth", "stderr", "err-a")
	_ = sink.AppendLine("task-auth", "stdout", "out-a")

	taskAuth, _ := os.ReadFile(filepath.Join(root, "task-auth.log"))
	if strings.Contains(string(taskAuth), "saas-backend") {
		t.Fatal("expected isolated log files per service")
	}
}
