package domain

import (
	"fmt"
	"strings"
	"time"
)

const (
	StreamStdout = "stdout"
	StreamStderr = "stderr"
)

type LogEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	ServiceName string    `json:"service_name"`
	Stream      string    `json:"stream"`
	Message     string    `json:"message"`
}

func NewLogEntry(timestamp time.Time, serviceName, stream, message string) (LogEntry, error) {
	if strings.TrimSpace(serviceName) == "" {
		return LogEntry{}, fmt.Errorf("service name is required")
	}
	if stream != StreamStdout && stream != StreamStderr {
		return LogEntry{}, fmt.Errorf("stream must be %q or %q", StreamStdout, StreamStderr)
	}
	if strings.TrimSpace(message) == "" {
		return LogEntry{}, fmt.Errorf("message is required")
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return LogEntry{
		Timestamp:   timestamp,
		ServiceName: serviceName,
		Stream:      stream,
		Message:     message,
	}, nil
}
