package main

import (
	"sort"
	"sync"
	"time"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusStarting Status = "starting"
	StatusRetrying Status = "retrying"
	StatusHealthy  Status = "healthy"
	StatusFailed   Status = "failed"
	StatusSkipped  Status = "skipped"
	StatusRestarting Status = "restarting"
	StatusBuilding  Status = "building"
)

type ServiceStatus struct {
	Name      string      `json:"name"`
	Status    Status      `json:"status"`
	DependsOn []DepStatus `json:"depends_on"`
	Command   string      `json:"command"`
	HealthPort string      `json:"health_port"`
	CommandPort string      `json:"command_port"`
	URL       string      `json:"url"`
	PID       int         `json:"pid"`
	StartedAt string      `json:"started_at"`
	LastChecked string      `json:"last_checked"`
	Error     string      `json:"error,omitempty"`
}

type DepStatus struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
}

type StatusStore struct {
	mu       sync.RWMutex
	services map[string]*ServiceStatus
}

func NewStatusStore() *StatusStore {
	return &StatusStore{
		services: make(map[string]*ServiceStatus),
	}
}

func (s *StatusStore) Init(names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range names {
		s.services[name] = &ServiceStatus{
			Name:   name,
			Status: StatusPending,
		}
	}
}

func (s *StatusStore) Update(name string, status Status, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc, ok := s.services[name]
	if !ok {
		return
	}
	svc.Status = status
	if status == StatusStarting && svc.StartedAt == "" {
		svc.StartedAt = time.Now().Format(time.RFC3339)
	}
	if errMsg != "" {
		svc.Error = errMsg
	} else if status == StatusHealthy {
		svc.Error = ""
	}
}

func (s *StatusStore) SetPID(name string, pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc, ok := s.services[name]; ok {
		svc.PID = pid
	}
}

func (s *StatusStore) SetDependsOn(name string, deps []DepStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc, ok := s.services[name]; ok {
		svc.DependsOn = deps
	}
}

func (s *StatusStore) SetCommand(name, command string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc, ok := s.services[name]; ok {
		svc.Command = command
	}
}

func (s *StatusStore) SetHealthPort(name, port string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc, ok := s.services[name]; ok {
		svc.HealthPort = port
	}
}

func (s *StatusStore) SetCommandPort(name, port string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc, ok := s.services[name]; ok {
		svc.CommandPort = port
	}
}

func (s *StatusStore) SetURL(name, url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc, ok := s.services[name]; ok {
		svc.URL = url
	}
}

func (s *StatusStore) SetLastChecked(name string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc, ok := s.services[name]; ok {
		svc.LastChecked = t.Format(time.RFC3339)
	}
}

func (s *StatusStore) CompareAndSwapStatus(name string, old, new Status) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc, ok := s.services[name]
	if !ok {
		return false
	}
	if svc.Status != old {
		return false
	}
	svc.Status = new
	return true
}

func (s *StatusStore) UpdateDependencyStatus(name string, status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, svc := range s.services {
		for i, dep := range svc.DependsOn {
			if dep.Name == name {
				svc.DependsOn[i].Status = status
			}
		}
	}
}

func (s *StatusStore) Get(name string) *ServiceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[name]
	if !ok {
		return nil
	}
	cp := *svc
	return &cp
}

func (s *StatusStore) All() []*ServiceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*ServiceStatus, 0, len(s.services))
	for _, svc := range s.services {
		result = append(result, svc)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}
