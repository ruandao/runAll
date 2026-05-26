package domain

import (
	"fmt"
	"strings"
)

type ServiceLogClipboardSnapshot struct {
	ServiceName string
	Lines       []string
}

func NewServiceLogClipboardSnapshot(serviceName string, entries []LogEntry) (ServiceLogClipboardSnapshot, error) {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return ServiceLogClipboardSnapshot{}, fmt.Errorf("service name is required")
	}
	if len(entries) == 0 {
		return ServiceLogClipboardSnapshot{}, fmt.Errorf("log entries are required")
	}

	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.ServiceName) == "" {
			return ServiceLogClipboardSnapshot{}, fmt.Errorf("log entry service name is required")
		}
		if entry.ServiceName != name {
			return ServiceLogClipboardSnapshot{}, fmt.Errorf(
				"log entry service mismatch: expected %q got %q",
				name,
				entry.ServiceName,
			)
		}
		lines = append(lines, renderLogEntryLine(entry))
	}

	return ServiceLogClipboardSnapshot{
		ServiceName: name,
		Lines:       lines,
	}, nil
}

func (s ServiceLogClipboardSnapshot) PlainText() string {
	if len(s.Lines) == 0 {
		return ""
	}
	return strings.Join(s.Lines, "\n")
}

func renderLogEntryLine(entry LogEntry) string {
	ts := entry.Timestamp.Format("15:04:05")
	stream := strings.TrimSpace(entry.Stream)
	if stream == "" {
		return fmt.Sprintf("[%s] %s", ts, entry.Message)
	}
	return fmt.Sprintf("[%s] (%s) %s", ts, stream, entry.Message)
}
