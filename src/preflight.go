package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"runAll/src/domain"
	"runAll/src/infrastructure"
)

func (r *Runner) newPreflightDomainService() domain.ServicePreflightDomainService {
	listenerFn := r.listenerPIDsFn
	if listenerFn == nil {
		listenerFn = listenerPIDs
	}
	return domain.NewServicePreflightDomainService(
		infrastructure.NewLsofPortListenerProbeRepository(listenerFn),
		infrastructure.NewSyscallForeignProcessTerminationRepository(),
		infrastructure.NewRunnerOwnedProcessRegistryRepository(r.ownedProcessPIDs),
	)
}

func (r *Runner) preflightService(ctx context.Context, svc Service) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	ports := resolveServicePorts(&svc)
	if len(ports) == 0 {
		return nil
	}

	service := r.newPreflightDomainService()
	results, err := service.EnsurePortsReadyForLaunch(svc.Name, ports, true)
	if err != nil {
		r.store.RecordPreflightFailure(svc.Name, domain.ServiceFailureCodePortConflict, err.Error())
		return fmt.Errorf("[%s] %s", svc.Name, err.Error())
	}

	for _, result := range results {
		if len(result.TerminatedPIDs) == 0 {
			continue
		}
		log.Printf("[%s] preflight cleaned foreign listeners on port %s (pid=%s)",
			svc.Name, result.Port, joinPIDs(result.TerminatedPIDs))
	}
	return nil
}

func (r *Runner) ownedProcessPIDs() map[int]struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	owned := make(map[int]struct{}, len(r.processes))
	for _, cmd := range r.processes {
		if cmd == nil || cmd.Process == nil {
			continue
		}
		owned[cmd.Process.Pid] = struct{}{}
	}
	return owned
}

func joinPIDs(pids []int) string {
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, fmt.Sprintf("%d", pid))
	}
	return strings.Join(parts, ",")
}
