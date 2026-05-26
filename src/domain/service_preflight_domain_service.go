package domain

import (
	"fmt"
	"strings"
)

type ServicePreflightDomainService struct {
	probeRepo         PortListenerProbeRepository
	terminationRepo   ForeignProcessTerminationRepository
	ownedRegistryRepo OwnedProcessRegistryRepository
}

func NewServicePreflightDomainService(
	probeRepo PortListenerProbeRepository,
	terminationRepo ForeignProcessTerminationRepository,
	ownedRegistryRepo OwnedProcessRegistryRepository,
) ServicePreflightDomainService {
	return ServicePreflightDomainService{
		probeRepo:         probeRepo,
		terminationRepo:   terminationRepo,
		ownedRegistryRepo: ownedRegistryRepo,
	}
}

func (s ServicePreflightDomainService) DetectPortConflicts(
	serviceName string,
	ports []string,
) ([]PortConflictSnapshot, error) {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return nil, fmt.Errorf("service name is required")
	}
	if s.probeRepo == nil {
		return nil, fmt.Errorf("port listener probe repository is required")
	}
	if s.ownedRegistryRepo == nil {
		return nil, fmt.Errorf("owned process registry repository is required")
	}

	owned := s.ownedRegistryRepo.OwnedPIDs()
	conflicts := make([]PortConflictSnapshot, 0, len(ports))
	seenPorts := make(map[string]struct{}, len(ports))

	for _, rawPort := range ports {
		port := strings.TrimSpace(rawPort)
		if port == "" {
			continue
		}
		if _, exists := seenPorts[port]; exists {
			continue
		}
		seenPorts[port] = struct{}{}

		listening, err := s.probeRepo.ListListeningPIDs(port)
		if err != nil {
			return nil, fmt.Errorf("probe listeners on port %s: %w", port, err)
		}
		foreign := FilterForeignPIDs(listening, owned)
		if len(foreign) == 0 {
			continue
		}

		snapshot, err := NewPortConflictSnapshot(name, port, foreign)
		if err != nil {
			return nil, err
		}
		conflicts = append(conflicts, snapshot)
	}

	return conflicts, nil
}

func (s ServicePreflightDomainService) ResolvePortConflicts(
	conflicts []PortConflictSnapshot,
) ([]PortConflictCleanupResult, error) {
	if len(conflicts) == 0 {
		return nil, nil
	}
	if s.probeRepo == nil {
		return nil, fmt.Errorf("port listener probe repository is required")
	}
	if s.terminationRepo == nil {
		return nil, fmt.Errorf("foreign process termination repository is required")
	}
	if s.ownedRegistryRepo == nil {
		return nil, fmt.Errorf("owned process registry repository is required")
	}

	owned := s.ownedRegistryRepo.OwnedPIDs()
	results := make([]PortConflictCleanupResult, 0, len(conflicts))

	for _, conflict := range conflicts {
		if err := s.terminationRepo.Terminate(conflict.ForeignPIDs); err != nil {
			return results, fmt.Errorf(
				"terminate foreign listeners on port %s (pid=%v): %w",
				conflict.Port,
				conflict.ForeignPIDs,
				err,
			)
		}

		remainingListening, err := s.probeRepo.ListListeningPIDs(conflict.Port)
		if err != nil {
			return results, fmt.Errorf("re-probe listeners on port %s: %w", conflict.Port, err)
		}
		remainingForeign := FilterForeignPIDs(remainingListening, owned)

		result, err := NewPortConflictCleanupResult(
			conflict.ServiceName,
			conflict.Port,
			conflict.ForeignPIDs,
			remainingForeign,
		)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}

	return results, nil
}

func (s ServicePreflightDomainService) BuildPortConflictFailureHint(
	conflict PortConflictSnapshot,
	cleanup PortConflictCleanupResult,
) (FailureHint, error) {
	message := fmt.Sprintf(
		"%s: service %s still has foreign listeners on port %s after cleanup",
		ServiceFailureCodePortConflict,
		conflict.ServiceName,
		conflict.Port,
	)
	if cleanup.Succeeded() {
		message = fmt.Sprintf(
			"%s: service %s cleaned foreign listeners on port %s",
			ServiceFailureCodePortConflict,
			conflict.ServiceName,
			conflict.Port,
		)
	}
	return NewFailureHint(
		ServiceFailureCodePortConflict,
		conflict.ServiceName,
		conflict.Port,
		cleanup.RemainingForeignPIDs,
		message,
	)
}

func (s ServicePreflightDomainService) EnsurePortsReadyForLaunch(
	serviceName string,
	ports []string,
	autoCleanup bool,
) ([]PortConflictCleanupResult, error) {
	conflicts, err := s.DetectPortConflicts(serviceName, ports)
	if err != nil {
		return nil, err
	}
	if len(conflicts) == 0 {
		return nil, nil
	}
	if !autoCleanup {
		conflict := conflicts[0]
		return nil, fmt.Errorf(
			"[%s] %s: service %s has foreign listeners on port %s (pid=%v)",
			conflict.ServiceName,
			ServiceFailureCodePortConflict,
			conflict.ServiceName,
			conflict.Port,
			conflict.ForeignPIDs,
		)
	}

	results, err := s.ResolvePortConflicts(conflicts)
	if err != nil {
		return results, err
	}
	for i, result := range results {
		if result.Succeeded() {
			continue
		}
		hint, hintErr := s.BuildPortConflictFailureHint(conflicts[i], result)
		if hintErr != nil {
			return results, hintErr
		}
		return results, fmt.Errorf("[%s] %s", result.ServiceName, hint.Render())
	}
	return results, nil
}
