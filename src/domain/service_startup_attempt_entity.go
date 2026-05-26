package domain

import (
	"fmt"
	"strings"
	"time"
)

type ServiceStartupAttempt struct {
	SessionID   string
	ServiceName string
	Phase       ServiceLifecyclePhase
	Success     bool
	FailureCode string
	Hint        string
	OccurredAt  time.Time
}

func NewServiceStartupAttempt(
	sessionID string,
	serviceName string,
	phase ServiceLifecyclePhase,
	success bool,
	failureCode string,
	hint string,
	occurredAt time.Time,
) (ServiceStartupAttempt, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return ServiceStartupAttempt{}, fmt.Errorf("session id is required")
	}
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return ServiceStartupAttempt{}, fmt.Errorf("service name is required")
	}
	code := strings.TrimSpace(failureCode)
	if !success {
		if _, err := NewServiceFailureCode(code); err != nil {
			return ServiceStartupAttempt{}, err
		}
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	return ServiceStartupAttempt{
		SessionID:   sid,
		ServiceName: name,
		Phase:       phase,
		Success:     success,
		FailureCode: code,
		Hint:        strings.TrimSpace(hint),
		OccurredAt:  occurredAt,
	}, nil
}
