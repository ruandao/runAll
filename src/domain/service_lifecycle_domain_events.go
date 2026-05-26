package domain

import "time"

type ServiceOwnershipAcquired struct {
	ServiceName    string
	OwnerSessionID string
	PID            int
	OccurredAt     time.Time
}

type ServiceStartRejectedByForeignProcess struct {
	ServiceName        string
	ActorSessionID     string
	ForeignPID         int
	ForeignCommandline string
	OccurredAt         time.Time
}

type ServicePreflightFailed struct {
	ServiceName string
	SessionID   string
	FailureCode string
	Hint        string
	OccurredAt  time.Time
}

type ServiceReadinessFailed struct {
	ServiceName string
	SessionID   string
	FailureCode string
	Hint        string
	OccurredAt  time.Time
}

type ServiceBecameHealthy struct {
	ServiceName string
	SessionID   string
	OccurredAt  time.Time
}

type PortConflictCleanupAttempted struct {
	ServiceName string
	Port        string
	ForeignPIDs []int
	OccurredAt  time.Time
}

type PortConflictCleanupSucceeded struct {
	ServiceName    string
	Port           string
	TerminatedPIDs []int
	OccurredAt     time.Time
}

type PortConflictCleanupFailed struct {
	ServiceName          string
	Port                 string
	RemainingForeignPIDs []int
	Hint                 string
	OccurredAt           time.Time
}
