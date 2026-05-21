package domain

import "testing"

type inMemoryServiceRuntimeContextRepository struct {
	services []ManagedService
}

func (r *inMemoryServiceRuntimeContextRepository) FindByName(name string) (ManagedService, error) {
	for _, service := range r.services {
		if service.Name == name {
			return service, nil
		}
	}
	return ManagedService{}, nil
}

func (r *inMemoryServiceRuntimeContextRepository) Save(service ManagedService) error {
	r.services = append(r.services, service)
	return nil
}

func (r *inMemoryServiceRuntimeContextRepository) ListByGroup(groupName string) ([]ManagedService, error) {
	var result []ManagedService
	for _, service := range r.services {
		if service.GroupName == groupName {
			result = append(result, service)
		}
	}
	return result, nil
}

func (r *inMemoryServiceRuntimeContextRepository) ListAll() ([]ManagedService, error) {
	return append([]ManagedService(nil), r.services...), nil
}

func TestServiceStopPolicyService_EvaluateStop(t *testing.T) {
	repository := &inMemoryServiceRuntimeContextRepository{
		services: []ManagedService{
			{Name: "db", Status: ServiceStatusHealthy},
			{Name: "api", Status: ServiceStatusHealthy, DependsOn: []string{"db"}},
		},
	}
	service := NewServiceStopPolicyService(repository)

	decision, err := service.EvaluateStop("db")
	if err != nil {
		t.Fatalf("EvaluateStop: %v", err)
	}
	if decision.Allow {
		t.Fatal("stop should be blocked when active dependent exists")
	}
	if len(decision.ActiveDependents) != 1 || decision.ActiveDependents[0] != "api" {
		t.Fatalf("unexpected dependents: %#v", decision.ActiveDependents)
	}
}
