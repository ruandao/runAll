package domain

import (
	"fmt"
	"strings"
)

const (
	ServiceLifecyclePhasePreflight = "preflight"
	ServiceLifecyclePhaseLaunch    = "launch"
	ServiceLifecyclePhaseReadiness = "readiness"
	ServiceLifecyclePhaseCompleted = "completed"
)

type ServiceLifecyclePhase struct {
	value string
}

func NewServiceLifecyclePhase(raw string) (ServiceLifecyclePhase, error) {
	v := strings.TrimSpace(strings.ToLower(raw))
	switch v {
	case ServiceLifecyclePhasePreflight,
		ServiceLifecyclePhaseLaunch,
		ServiceLifecyclePhaseReadiness,
		ServiceLifecyclePhaseCompleted:
		return ServiceLifecyclePhase{value: v}, nil
	default:
		return ServiceLifecyclePhase{}, fmt.Errorf("invalid service lifecycle phase: %q", raw)
	}
}

func (p ServiceLifecyclePhase) Value() string {
	return p.value
}
