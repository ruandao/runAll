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

type ServiceStopRequested struct {
	ServiceName string
	OccurredAt  time.Time
}

type ServiceStopped struct {
	ServiceName string
	OccurredAt  time.Time
}

type ServiceStartRequested struct {
	ServiceName string
	OccurredAt  time.Time
}

type ServiceStarted struct {
	ServiceName string
	OccurredAt  time.Time
}

type ServiceGroupStopRequested struct {
	GroupName  string
	OccurredAt time.Time
}

type ServiceCascadeStartRequested struct {
	TargetServiceName string
	PlannedOrder      []string
	OccurredAt        time.Time
}

type ServiceCascadeStopRequested struct {
	TargetServiceName string
	PlannedOrder      []string
	OccurredAt        time.Time
}

type ServiceGroupStartRequested struct {
	GroupName    string
	PlannedOrder []string
	OccurredAt   time.Time
}

type ServiceCascadeStepFailed struct {
	Operation string
	FailedAt  string
	Completed []string
	Error     string
	OccurredAt time.Time
}
