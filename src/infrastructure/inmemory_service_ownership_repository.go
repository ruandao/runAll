package infrastructure

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"runAll/src/domain"
)

type InMemoryServiceOwnershipRepository struct {
	mu         sync.RWMutex
	ownerships map[string]domain.ServiceOwnership
}

func NewInMemoryServiceOwnershipRepository() *InMemoryServiceOwnershipRepository {
	return &InMemoryServiceOwnershipRepository{
		ownerships: make(map[string]domain.ServiceOwnership),
	}
}

func (r *InMemoryServiceOwnershipRepository) FindByServiceName(serviceName string) (domain.ServiceOwnership, error) {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return domain.ServiceOwnership{}, fmt.Errorf("service name is required")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	ownership, exists := r.ownerships[name]
	if !exists {
		return domain.ServiceOwnership{}, nil
	}
	return ownership, nil
}

func (r *InMemoryServiceOwnershipRepository) Save(ownership domain.ServiceOwnership) error {
	name := strings.TrimSpace(ownership.ServiceName)
	if name == "" {
		return fmt.Errorf("service name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.ownerships[name] = ownership
	return nil
}

func (r *InMemoryServiceOwnershipRepository) DeleteByServiceName(serviceName string) error {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return fmt.Errorf("service name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.ownerships, name)
	return nil
}

func (r *InMemoryServiceOwnershipRepository) ListAll() ([]domain.ServiceOwnership, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]domain.ServiceOwnership, 0, len(r.ownerships))
	for _, ownership := range r.ownerships {
		result = append(result, ownership)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ServiceName < result[j].ServiceName
	})
	return result, nil
}
