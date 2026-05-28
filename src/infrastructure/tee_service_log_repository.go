package infrastructure

import (
	"log"

	"runAll/src/domain"
)

type TeeServiceLogRepository struct {
	primary domain.ServiceLogRepository
	file    domain.ServiceLogFileSink
}

func NewTeeServiceLogRepository(primary domain.ServiceLogRepository, file domain.ServiceLogFileSink) domain.ServiceLogRepository {
	if primary == nil {
		primary = NewInMemoryServiceLogRepository(DefaultServiceLogCapacity)
	}
	return &TeeServiceLogRepository{
		primary: primary,
		file:    file,
	}
}

func (r *TeeServiceLogRepository) Append(service string, entry domain.LogEntry) {
	if r == nil || r.primary == nil {
		return
	}
	r.primary.Append(service, entry)
	if r.file == nil {
		return
	}
	if err := r.file.AppendLine(service, entry.Stream, entry.Message); err != nil {
		log.Printf("[runAll] file log sink append failed service=%s: %v", service, err)
	}
}

func (r *TeeServiceLogRepository) Tail(service string, lines int) []domain.LogEntry {
	if r == nil || r.primary == nil {
		return []domain.LogEntry{}
	}
	return r.primary.Tail(service, lines)
}

func (r *TeeServiceLogRepository) Clear(service string) {
	if r == nil || r.primary == nil {
		return
	}
	r.primary.Clear(service)
}
