package domain

import "time"

type ServiceBuildStarted struct {
	ServiceName string
	OccurredAt  time.Time
}

type ServiceBuildFinished struct {
	ServiceName string
	OccurredAt  time.Time
	Success     bool
	Error       string
}

type ServiceLogsRead struct {
	ServiceName string
	OccurredAt  time.Time
	Lines       int
}
