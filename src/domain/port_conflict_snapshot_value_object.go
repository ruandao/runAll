package domain

import (
	"fmt"
	"sort"
	"strings"
)

type PortConflictSnapshot struct {
	ServiceName string
	Port        string
	ForeignPIDs []int
}

func NewPortConflictSnapshot(serviceName, port string, foreignPIDs []int) (PortConflictSnapshot, error) {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return PortConflictSnapshot{}, fmt.Errorf("service name is required")
	}
	normalizedPort := strings.TrimSpace(port)
	if normalizedPort == "" {
		return PortConflictSnapshot{}, fmt.Errorf("port is required")
	}
	if !isValidPort(normalizedPort) {
		return PortConflictSnapshot{}, fmt.Errorf("invalid port %q", port)
	}

	normalized := normalizePIDList(foreignPIDs)
	if len(normalized) == 0 {
		return PortConflictSnapshot{}, fmt.Errorf("foreign pids are required for port conflict snapshot")
	}

	return PortConflictSnapshot{
		ServiceName: name,
		Port:        normalizedPort,
		ForeignPIDs: normalized,
	}, nil
}

func FilterForeignPIDs(listening []int, owned map[int]struct{}) []int {
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
	sort.Ints(result)
	return result
}

func normalizePIDList(pids []int) []int {
	seen := make(map[int]struct{}, len(pids))
	result := make([]int, 0, len(pids))
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		result = append(result, pid)
	}
	sort.Ints(result)
	return result
}
