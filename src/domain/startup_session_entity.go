package domain

import (
	"fmt"
	"strings"
	"time"
)

const (
	StartupSessionStatusRunning  = "running"
	StartupSessionStatusFailed   = "failed"
	StartupSessionStatusFinished = "finished"
)

type StartupSession struct {
	ID         string
	ConfigPath string
	ConfigHash string
	Status     string
	StartedAt  time.Time
	UpdatedAt  time.Time
}

func NewStartupSession(id, configPath, configHash string, startedAt time.Time) (StartupSession, error) {
	sid := strings.TrimSpace(id)
	if sid == "" {
		return StartupSession{}, fmt.Errorf("startup session id is required")
	}
	path := strings.TrimSpace(configPath)
	if path == "" {
		return StartupSession{}, fmt.Errorf("config path is required")
	}
	hash := strings.TrimSpace(configHash)
	if hash == "" {
		return StartupSession{}, fmt.Errorf("config hash is required")
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return StartupSession{
		ID:         sid,
		ConfigPath: path,
		ConfigHash: hash,
		Status:     StartupSessionStatusRunning,
		StartedAt:  startedAt,
		UpdatedAt:  startedAt,
	}, nil
}

func (s StartupSession) MarkFailed(at time.Time) StartupSession {
	cp := s
	cp.Status = StartupSessionStatusFailed
	if at.IsZero() {
		at = time.Now()
	}
	cp.UpdatedAt = at
	return cp
}

func (s StartupSession) MarkFinished(at time.Time) StartupSession {
	cp := s
	cp.Status = StartupSessionStatusFinished
	if at.IsZero() {
		at = time.Now()
	}
	cp.UpdatedAt = at
	return cp
}
