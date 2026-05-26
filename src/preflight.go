package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"syscall"
	"time"

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
		logPIDs := joinPIDs(foreign)
		if err := terminatePIDs(foreign); err != nil {
			msg := fmt.Sprintf(
				"%s: service %s failed to terminate foreign listeners on port %s (pid=%s): %v",
				domain.ServiceFailureCodePortConflict,
				svc.Name,
				port,
				logPIDs,
				err,
			)
			r.store.RecordPreflightFailure(svc.Name, domain.ServiceFailureCodePortConflict, msg)
			return fmt.Errorf("[%s] %s", svc.Name, msg)
		}

		remaining, err := listenerPIDs(port)
		if err != nil {
			return fmt.Errorf("[%s] preflight listener re-scan failed on port %s: %w", svc.Name, port, err)
		}
		remainingForeign := filterForeignPIDs(remaining, owned)
		if len(remainingForeign) != 0 {
			sort.Ints(remainingForeign)
			msg := fmt.Sprintf(
				"%s: service %s still has foreign listeners on port %s after cleanup (pid=%s)",
				domain.ServiceFailureCodePortConflict,
				svc.Name,
				port,
				joinPIDs(remainingForeign),
			)
			r.store.RecordPreflightFailure(svc.Name, domain.ServiceFailureCodePortConflict, msg)
			return fmt.Errorf("[%s] %s", svc.Name, msg)
		}
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

func terminatePIDs(pids []int) error {
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
			return err
		}
	}

	time.Sleep(250 * time.Millisecond)

	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if err := syscall.Kill(pid, 0); err == nil {
			if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil && killErr != syscall.ESRCH {
				return killErr
			}
		}
	}
	return nil
}
