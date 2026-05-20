package domain

type ServiceLogRepository interface {
	Append(service string, entry LogEntry)
	Tail(service string, lines int) []LogEntry
}
