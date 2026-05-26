package domain

import (
	"fmt"
	"strings"
)

type PortConflictCleanupResult struct {
	ServiceName          string
	Port                 string
	TerminatedPIDs       []int
	RemainingForeignPIDs []int
}

func NewPortConflictCleanupResult(
	serviceName string,
	port string,
	terminatedPIDs []int,
	remainingForeignPIDs []int,
) (PortConflictCleanupResult, error) {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return PortConflictCleanupResult{}, fmt.Errorf("service name is required")
	}
	normalizedPort := strings.TrimSpace(port)
	if normalizedPort == "" {
		return PortConflictCleanupResult{}, fmt.Errorf("port is required")
	}
	if !isValidPort(normalizedPort) {
		return PortConflictCleanupResult{}, fmt.Errorf("invalid port %q", port)
	}

	return PortConflictCleanupResult{
		ServiceName:          name,
		Port:                 normalizedPort,
		TerminatedPIDs:       normalizePIDList(terminatedPIDs),
		RemainingForeignPIDs: normalizePIDList(remainingForeignPIDs),
	}, nil
}

func (r PortConflictCleanupResult) Succeeded() bool {
	return len(r.RemainingForeignPIDs) == 0
}
