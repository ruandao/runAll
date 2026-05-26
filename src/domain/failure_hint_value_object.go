package domain

import (
	"fmt"
	"sort"
	"strings"
)

type FailureHint struct {
	FailureCode string
	ServiceName string
	Port        string
	ForeignPIDs []int
	Message     string
}

func NewFailureHint(
	failureCode string,
	serviceName string,
	port string,
	foreignPIDs []int,
	message string,
) (FailureHint, error) {
	code, err := NewServiceFailureCode(failureCode)
	if err != nil {
		return FailureHint{}, err
	}
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return FailureHint{}, fmt.Errorf("service name is required")
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return FailureHint{}, fmt.Errorf("failure hint message is required")
	}

	normalizedPort := strings.TrimSpace(port)
	if normalizedPort != "" && !isValidPort(normalizedPort) {
		return FailureHint{}, fmt.Errorf("invalid port %q", port)
	}

	return FailureHint{
		FailureCode: code.Value(),
		ServiceName: name,
		Port:        normalizedPort,
		ForeignPIDs: normalizePIDList(foreignPIDs),
		Message:     msg,
	}, nil
}

func (h FailureHint) Render() string {
	parts := []string{h.Message}
	if h.Port != "" {
		parts = append(parts, fmt.Sprintf("port=%s", h.Port))
	}
	if len(h.ForeignPIDs) > 0 {
		pids := append([]int(nil), h.ForeignPIDs...)
		sort.Ints(pids)
		pidParts := make([]string, 0, len(pids))
		for _, pid := range pids {
			pidParts = append(pidParts, fmt.Sprintf("%d", pid))
		}
		parts = append(parts, fmt.Sprintf("pid=%s", strings.Join(pidParts, ",")))
	}
	return strings.Join(parts, "; ")
}
