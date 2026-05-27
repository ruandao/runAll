package domain

import "testing"

type inMemoryServiceTopologyRepository struct {
	nodes []ServiceTopologyNode
}

func (r *inMemoryServiceTopologyRepository) ListAll() ([]ServiceTopologyNode, error) {
	return append([]ServiceTopologyNode(nil), r.nodes...), nil
}

func TestServiceCascadeOrchestrationService_PlanStartCascade(t *testing.T) {
	topology := &inMemoryServiceTopologyRepository{
		nodes: []ServiceTopologyNode{
			{Name: "a", DependsOn: nil},
			{Name: "b", DependsOn: []string{"a"}},
			{Name: "c", DependsOn: []string{"b"}},
		},
	}
	runtime := &inMemoryServiceRuntimeContextRepository{
		services: []ManagedService{
			{Name: "a", Status: ServiceStatusStopped},
			{Name: "b", Status: ServiceStatusStopped},
			{Name: "c", Status: ServiceStatusStopped},
		},
	}
	service := NewServiceCascadeOrchestrationService(topology, runtime)

	plan, err := service.PlanStartCascade("c")
	if err != nil {
		t.Fatalf("PlanStartCascade: %v", err)
	}
	if plan.Operation != LifecycleOperationStart {
		t.Fatalf("operation = %q, want start", plan.Operation)
	}
	want := []string{"a", "b", "c"}
	if len(plan.OrderedNames) != len(want) {
		t.Fatalf("ordered names = %#v, want %#v", plan.OrderedNames, want)
	}
	for i, name := range want {
		if plan.OrderedNames[i] != name {
			t.Fatalf("ordered names = %#v, want %#v", plan.OrderedNames, want)
		}
	}
}

func TestServiceCascadeOrchestrationService_PlanStartCascade_SkipsHealthyUpstream(t *testing.T) {
	topology := &inMemoryServiceTopologyRepository{
		nodes: []ServiceTopologyNode{
			{Name: "a", DependsOn: nil},
			{Name: "b", DependsOn: []string{"a"}},
			{Name: "c", DependsOn: []string{"b"}},
		},
	}
	runtime := &inMemoryServiceRuntimeContextRepository{
		services: []ManagedService{
			{Name: "a", Status: ServiceStatusHealthy},
			{Name: "b", Status: ServiceStatusStopped},
			{Name: "c", Status: ServiceStatusStopped},
		},
	}
	service := NewServiceCascadeOrchestrationService(topology, runtime)

	plan, err := service.PlanStartCascade("c")
	if err != nil {
		t.Fatalf("PlanStartCascade: %v", err)
	}
	want := []string{"b", "c"}
	if len(plan.OrderedNames) != len(want) {
		t.Fatalf("ordered names = %#v, want %#v", plan.OrderedNames, want)
	}
}

func TestServiceCascadeOrchestrationService_PlanStopCascade(t *testing.T) {
	topology := &inMemoryServiceTopologyRepository{
		nodes: []ServiceTopologyNode{
			{Name: "a", DependsOn: nil},
			{Name: "b", DependsOn: []string{"a"}},
			{Name: "c", DependsOn: []string{"b"}},
		},
	}
	runtime := &inMemoryServiceRuntimeContextRepository{
		services: []ManagedService{
			{Name: "a", Status: ServiceStatusHealthy},
			{Name: "b", Status: ServiceStatusHealthy},
			{Name: "c", Status: ServiceStatusHealthy},
		},
	}
	service := NewServiceCascadeOrchestrationService(topology, runtime)

	plan, err := service.PlanStopCascade("a")
	if err != nil {
		t.Fatalf("PlanStopCascade: %v", err)
	}
	want := []string{"c", "b", "a"}
	if len(plan.OrderedNames) != len(want) {
		t.Fatalf("ordered names = %#v, want %#v", plan.OrderedNames, want)
	}
	for i, name := range want {
		if plan.OrderedNames[i] != name {
			t.Fatalf("ordered names = %#v, want %#v", plan.OrderedNames, want)
		}
	}
}

func TestServiceCascadeOrchestrationService_PlanStartGroup(t *testing.T) {
	topology := &inMemoryServiceTopologyRepository{
		nodes: []ServiceTopologyNode{
			{Name: "a", GroupName: "g1", DependsOn: nil},
			{Name: "b", GroupName: "g1", DependsOn: []string{"a"}},
			{Name: "x", GroupName: "g2", DependsOn: []string{"a"}},
		},
	}
	runtime := &inMemoryServiceRuntimeContextRepository{
		services: []ManagedService{
			{Name: "a", GroupName: "g1", Status: ServiceStatusStopped},
			{Name: "b", GroupName: "g1", Status: ServiceStatusStopped},
			{Name: "x", GroupName: "g2", Status: ServiceStatusStopped},
		},
	}
	service := NewServiceCascadeOrchestrationService(topology, runtime)

	plan, err := service.PlanStartGroup("g1")
	if err != nil {
		t.Fatalf("PlanStartGroup: %v", err)
	}
	want := []string{"a", "b"}
	if len(plan.OrderedNames) != len(want) {
		t.Fatalf("ordered names = %#v, want %#v", plan.OrderedNames, want)
	}
}

func TestServiceCascadeOrchestrationService_PlanStartGroup_IncludesExternalUpstream(t *testing.T) {
	topology := &inMemoryServiceTopologyRepository{
		nodes: []ServiceTopologyNode{
			{Name: "upstream", GroupName: "infra", DependsOn: nil},
			{Name: "api", GroupName: "platform", DependsOn: []string{"upstream"}},
		},
	}
	runtime := &inMemoryServiceRuntimeContextRepository{
		services: []ManagedService{
			{Name: "upstream", GroupName: "infra", Status: ServiceStatusStopped},
			{Name: "api", GroupName: "platform", Status: ServiceStatusStopped},
		},
	}
	service := NewServiceCascadeOrchestrationService(topology, runtime)

	plan, err := service.PlanStartGroup("platform")
	if err != nil {
		t.Fatalf("PlanStartGroup: %v", err)
	}
	want := []string{"upstream", "api"}
	if len(plan.OrderedNames) != len(want) {
		t.Fatalf("ordered names = %#v, want %#v", plan.OrderedNames, want)
	}
}
