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

func TestServiceCascadeOrchestrationService_PlanStartCascade_IncludesPendingUpstream(t *testing.T) {
	topology := &inMemoryServiceTopologyRepository{
		nodes: []ServiceTopologyNode{
			{Name: "git-oauth", DependsOn: nil},
			{Name: "saas-backend", DependsOn: []string{"git-oauth"}},
			{Name: "vue-frontend", DependsOn: []string{"saas-backend"}},
		},
	}
	runtime := &inMemoryServiceRuntimeContextRepository{
		services: []ManagedService{
			{Name: "git-oauth", Status: ServiceStatusHealthy},
			{Name: "saas-backend", Status: ServiceStatusPending},
			{Name: "vue-frontend", Status: ServiceStatusPending},
		},
	}
	service := NewServiceCascadeOrchestrationService(topology, runtime)

	plan, err := service.PlanStartCascade("vue-frontend")
	if err != nil {
		t.Fatalf("PlanStartCascade: %v", err)
	}
	want := []string{"saas-backend", "vue-frontend"}
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

func TestServiceCascadeOrchestrationService_PlanStopCascade_IncludesFailedDownstream(t *testing.T) {
	topology := &inMemoryServiceTopologyRepository{
		nodes: []ServiceTopologyNode{
			{Name: "git-oauth", DependsOn: nil},
			{Name: "saas-backend", DependsOn: []string{"git-oauth"}},
			{Name: "vue-frontend", DependsOn: []string{"saas-backend"}},
			{Name: "ai-provider", DependsOn: []string{"saas-backend"}},
		},
	}
	runtime := &inMemoryServiceRuntimeContextRepository{
		services: []ManagedService{
			{Name: "git-oauth", Status: ServiceStatusHealthy},
			{Name: "saas-backend", Status: ServiceStatusFailed},
			{Name: "vue-frontend", Status: ServiceStatusStopped},
			{Name: "ai-provider", Status: ServiceStatusHealthy},
		},
	}
	service := NewServiceCascadeOrchestrationService(topology, runtime)

	plan, err := service.PlanStopCascade("git-oauth")
	if err != nil {
		t.Fatalf("PlanStopCascade: %v", err)
	}
	want := []string{"ai-provider", "saas-backend", "git-oauth"}
	if len(plan.OrderedNames) != len(want) {
		t.Fatalf("ordered names = %#v, want %#v", plan.OrderedNames, want)
	}
	for i, name := range want {
		if plan.OrderedNames[i] != name {
			t.Fatalf("ordered names = %#v, want %#v", plan.OrderedNames, want)
		}
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
