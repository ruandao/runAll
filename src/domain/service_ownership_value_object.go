package domain

import (
	"fmt"
	"strings"
	"time"
)

type ServiceOwnership struct {
	ServiceName      string
	OwnerSessionID   string
	PID              int
	ConfigHash       string
	HealthEndpoint   string
	OwnershipTakenAt time.Time
}

func NewServiceOwnership(
	serviceName string,
	ownerSessionID string,
	pid int,
	configHash string,
	healthEndpoint string,
	takenAt time.Time,
) (ServiceOwnership, error) {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return ServiceOwnership{}, fmt.Errorf("service name is required")
	}
	sessionID := strings.TrimSpace(ownerSessionID)
	if sessionID == "" {
		return ServiceOwnership{}, fmt.Errorf("owner session id is required")
	}
	if pid <= 0 {
		return ServiceOwnership{}, fmt.Errorf("pid must be positive")
	}
	hash := strings.TrimSpace(configHash)
	if hash == "" {
		return ServiceOwnership{}, fmt.Errorf("config hash is required")
	}
	endpoint := strings.TrimSpace(healthEndpoint)
	if endpoint == "" {
		return ServiceOwnership{}, fmt.Errorf("health endpoint is required")
	}
	if takenAt.IsZero() {
		takenAt = time.Now()
	}
	return ServiceOwnership{
		ServiceName:      name,
		OwnerSessionID:   sessionID,
		PID:              pid,
		ConfigHash:       hash,
		HealthEndpoint:   endpoint,
		OwnershipTakenAt: takenAt,
	}, nil
}

func (o ServiceOwnership) BelongsTo(ownerSessionID string) bool {
	return strings.TrimSpace(o.OwnerSessionID) != "" &&
		o.OwnerSessionID == strings.TrimSpace(ownerSessionID)
}
