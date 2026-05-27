package domain

import (
	"fmt"
	"strings"
)

const (
	ServiceStatusPending    = "pending"
	ServiceStatusStarting   = "starting"
	ServiceStatusRetrying   = "retrying"
	ServiceStatusHealthy    = "healthy"
	ServiceStatusFailed     = "failed"
	ServiceStatusSkipped    = "skipped"
	ServiceStatusRestarting = "restarting"
	ServiceStatusBuilding   = "building"
	ServiceStatusStopped    = "stopped"
)

type ManagedService struct {
	Name      string
	GroupName string
	Status    string
	DependsOn []string
}

func NewManagedService(name, groupName, status string, dependsOn []string) (ManagedService, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ManagedService{}, fmt.Errorf("service name is required")
	}
	if status == "" {
		status = ServiceStatusPending
	}
	if !isKnownServiceStatus(status) {
		return ManagedService{}, fmt.Errorf("unknown status %q", status)
	}
	return ManagedService{
		Name:      name,
		GroupName: strings.TrimSpace(groupName),
		Status:    status,
		DependsOn: append([]string(nil), dependsOn...),
	}, nil
}

func (s ManagedService) CanStart() bool {
	return IsStartableServiceStatus(s.Status)
}

// IsStartableServiceStatus reports whether a service is idle enough to launch.
func IsStartableServiceStatus(status string) bool {
	switch status {
	case ServiceStatusStopped, ServiceStatusFailed, ServiceStatusSkipped, ServiceStatusPending:
		return true
	default:
		return false
	}
}

func (s ManagedService) CanStop(activeDependents []string) error {
	if len(activeDependents) > 0 {
		return fmt.Errorf("service %q has active downstream dependencies: %s", s.Name, strings.Join(activeDependents, ", "))
	}
	switch s.Status {
	case ServiceStatusHealthy, ServiceStatusFailed, ServiceStatusRetrying, ServiceStatusStopped:
		return nil
	default:
		return fmt.Errorf("service %q is %s, can only stop healthy, failed, or retrying services", s.Name, s.Status)
	}
}

func (s ManagedService) WithStopped() ManagedService {
	cp := s
	cp.Status = ServiceStatusStopped
	return cp
}

func isKnownServiceStatus(status string) bool {
	switch status {
	case ServiceStatusPending,
		ServiceStatusStarting,
		ServiceStatusRetrying,
		ServiceStatusHealthy,
		ServiceStatusFailed,
		ServiceStatusSkipped,
		ServiceStatusRestarting,
		ServiceStatusBuilding,
		ServiceStatusStopped:
		return true
	default:
		return false
	}
}
