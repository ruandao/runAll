package infrastructure

import (
	"sync"

	"runAll/src/domain"
)

const DefaultServiceLogCapacity = 1000

type InMemoryServiceLogRepository struct {
	mu       sync.RWMutex
	capacity int
	buffers  map[string][]domain.LogEntry
}

func NewInMemoryServiceLogRepository(capacity int) *InMemoryServiceLogRepository {
	if capacity <= 0 {
		capacity = DefaultServiceLogCapacity
	}
	return &InMemoryServiceLogRepository{
		capacity: capacity,
		buffers:  make(map[string][]domain.LogEntry),
	}
}

func (r *InMemoryServiceLogRepository) Append(service string, entry domain.LogEntry) {
	if service == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	buffer := append(r.buffers[service], entry)
	if len(buffer) > r.capacity {
		buffer = append([]domain.LogEntry(nil), buffer[len(buffer)-r.capacity:]...)
	}
	r.buffers[service] = buffer
}

func (r *InMemoryServiceLogRepository) Tail(service string, lines int) []domain.LogEntry {
	if service == "" || lines <= 0 {
		return []domain.LogEntry{}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	buffer := r.buffers[service]
	if len(buffer) == 0 {
		return []domain.LogEntry{}
	}

	if lines >= len(buffer) {
		return append([]domain.LogEntry(nil), buffer...)
	}
	return append([]domain.LogEntry(nil), buffer[len(buffer)-lines:]...)
}
