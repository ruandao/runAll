package main

import (
	"context"
	"encoding/json"
	"io"
	"strings"
)

const (
	doctorExitSuccess         = 0
	doctorExitPreflightFailed = 2
)

type doctorServiceReport struct {
	Name         string `json:"name"`
	Status       Status `json:"status"`
	Phase        string `json:"phase,omitempty"`
	FailurePhase string `json:"failure_phase,omitempty"`
	FailureCode  string `json:"failure_code,omitempty"`
	Hint         string `json:"hint,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
}

type doctorReport struct {
	Command  string                `json:"command"`
	Services []doctorServiceReport `json:"services"`
}

func RunDoctor(ctx context.Context, runner *Runner, out io.Writer) int {
	if runner == nil || out == nil {
		return doctorExitPreflightFailed
	}

	services := runner.cfg.Flatten()
	report := doctorReport{
		Command:  "doctor",
		Services: make([]doctorServiceReport, 0, len(services)),
	}

	hasFailure := false
	for _, svc := range services {
		err := runner.runPreflight(ctx, svc)
		if err != nil {
			hasFailure = true
		}

		current := runner.store.Get(svc.Name)
		entry := doctorServiceReport{
			Name:   svc.Name,
			Status: StatusPending,
		}
		if current != nil {
			entry.Status = current.Status
			entry.Phase = current.Phase
			entry.FailurePhase = current.FailurePhase
			entry.FailureCode = current.FailureCode
			entry.Hint = deriveFailureHint(current)
		}
		entry.SessionID = resolveServiceSessionID(runner, svc.Name)
		if entry.Hint == "" && err != nil {
			entry.Hint = strings.TrimSpace(err.Error())
		}
		report.Services = append(report.Services, entry)
	}

	_ = json.NewEncoder(out).Encode(report)
	if hasFailure {
		return doctorExitPreflightFailed
	}
	return doctorExitSuccess
}

func deriveFailureHint(status *ServiceStatus) string {
	if status == nil {
		return ""
	}
	return strings.TrimSpace(status.Error)
}

func resolveServiceSessionID(runner *Runner, serviceName string) string {
	if runner == nil || runner.ownershipRepo == nil {
		return ""
	}
	ownership, err := runner.ownershipRepo.FindByServiceName(serviceName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(ownership.OwnerSessionID)
}
