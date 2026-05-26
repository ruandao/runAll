package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"runAll/src/domain"
)

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

	owned := r.ownedProcessPIDs()
	for _, port := range ports {
		pids, err := listenerPIDs(port)
		if err != nil {
			return fmt.Errorf("[%s] preflight listener scan failed on port %s: %w", svc.Name, port, err)
		}
		foreign := filterForeignPIDs(pids, owned)
		if len(foreign) == 0 {
			continue
		}

		sort.Ints(foreign)
		msg := fmt.Sprintf(
			"%s: service %s has foreign listeners on port %s (pid=%s)",
			domain.ServiceFailureCodePortConflict,
			svc.Name,
			port,
			joinPIDs(foreign),
		)
		r.store.RecordPreflightFailure(svc.Name, domain.ServiceFailureCodePortConflict, msg)
		return fmt.Errorf("[%s] %s", svc.Name, msg)
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

func filterForeignPIDs(listening []int, owned map[int]struct{}) []int {
	result := make([]int, 0, len(listening))
	seen := make(map[int]struct{}, len(listening))
	for _, pid := range listening {
		if pid <= 0 {
			continue
		}
		if _, ok := owned[pid]; ok {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		result = append(result, pid)
	}
	return result
}

func joinPIDs(pids []int) string {
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, fmt.Sprintf("%d", pid))
	}
	return strings.Join(parts, ",")
}
