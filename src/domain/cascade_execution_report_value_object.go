package domain

import (
	"fmt"
	"strings"
)

// CascadeExecutionReport captures partial progress when a cascade operation fails.
type CascadeExecutionReport struct {
	Completed []string
	FailedAt  string
}

func NewCascadeExecutionReport(completed []string, failedAt string) (CascadeExecutionReport, error) {
	failedAt = strings.TrimSpace(failedAt)
	if failedAt == "" {
		return CascadeExecutionReport{}, fmt.Errorf("cascade failed_at is required")
	}
	return CascadeExecutionReport{
		Completed: append([]string(nil), completed...),
		FailedAt:  failedAt,
	}, nil
}
