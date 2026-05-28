package domain

// ServiceLogFileSink persists raw log lines for centralized observability (e.g. Loki/Promtail).
type ServiceLogFileSink interface {
	AppendLine(serviceName, stream, message string) error
	Close() error
}
