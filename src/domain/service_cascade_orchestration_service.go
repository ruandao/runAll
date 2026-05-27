package domain

import (
	"fmt"
	"sort"
	"strings"
)

type ServiceCascadeOrchestrationService struct {
	topology ServiceTopologyRepository
	runtime  ServiceRuntimeContextRepository
}

func NewServiceCascadeOrchestrationService(
	topology ServiceTopologyRepository,
	runtime ServiceRuntimeContextRepository,
) *ServiceCascadeOrchestrationService {
	return &ServiceCascadeOrchestrationService{
		topology: topology,
		runtime:  runtime,
	}
}

func (s *ServiceCascadeOrchestrationService) PlanStartCascade(targetName string) (ServiceLifecyclePlan, error) {
	targetName = strings.TrimSpace(targetName)
	if targetName == "" {
		return ServiceLifecyclePlan{}, fmt.Errorf("target service name is required")
	}

	_, index, err := s.loadTopologyIndex()
	if err != nil {
		return ServiceLifecyclePlan{}, err
	}
	if _, ok := index[targetName]; !ok {
		return ServiceLifecyclePlan{}, fmt.Errorf("service %q not found", targetName)
	}

	closure := collectUpstream(targetName, index)
	order, err := topologicalOrder(closure, index)
	if err != nil {
		return ServiceLifecyclePlan{}, err
	}

	filtered, err := s.filterStartable(order)
	if err != nil {
		return ServiceLifecyclePlan{}, err
	}
	return NewServiceLifecyclePlan(LifecycleOperationStart, filtered)
}

func (s *ServiceCascadeOrchestrationService) PlanStopCascade(targetName string) (ServiceLifecyclePlan, error) {
	targetName = strings.TrimSpace(targetName)
	if targetName == "" {
		return ServiceLifecyclePlan{}, fmt.Errorf("target service name is required")
	}

	nodes, index, err := s.loadTopologyIndex()
	if err != nil {
		return ServiceLifecyclePlan{}, err
	}
	if _, ok := index[targetName]; !ok {
		return ServiceLifecyclePlan{}, fmt.Errorf("service %q not found", targetName)
	}

	dependents := buildDependentsIndex(nodes)
	closure := collectDownstream(targetName, dependents)
	order, err := topologicalOrder(closure, index)
	if err != nil {
		return ServiceLifecyclePlan{}, err
	}

	stopOrder := reverseNames(order)
	filtered, err := s.filterStoppable(stopOrder, targetName)
	if err != nil {
		return ServiceLifecyclePlan{}, err
	}
	return NewServiceLifecyclePlan(LifecycleOperationStop, filtered)
}

func (s *ServiceCascadeOrchestrationService) PlanStartGroup(groupName string) (ServiceLifecyclePlan, error) {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return ServiceLifecyclePlan{}, fmt.Errorf("group name is required")
	}

	nodes, index, err := s.loadTopologyIndex()
	if err != nil {
		return ServiceLifecyclePlan{}, err
	}

	closure := make(map[string]struct{})
	for _, node := range nodes {
		if node.GroupName != groupName {
			continue
		}
		closure[node.Name] = struct{}{}
		for _, dep := range node.DependsOn {
			closure[dep] = struct{}{}
		}
	}
	if len(closure) == 0 {
		return ServiceLifecyclePlan{}, fmt.Errorf("group %q not found", groupName)
	}

	order, err := topologicalOrder(closure, index)
	if err != nil {
		return ServiceLifecyclePlan{}, err
	}

	filtered, err := s.filterStartable(order)
	if err != nil {
		return ServiceLifecyclePlan{}, err
	}
	return NewServiceLifecyclePlan(LifecycleOperationStart, filtered)
}

func (s *ServiceCascadeOrchestrationService) loadTopologyIndex() ([]ServiceTopologyNode, map[string]ServiceTopologyNode, error) {
	nodes, err := s.topology.ListAll()
	if err != nil {
		return nil, nil, err
	}
	index := make(map[string]ServiceTopologyNode, len(nodes))
	for _, node := range nodes {
		index[node.Name] = node
	}
	return nodes, index, nil
}

func (s *ServiceCascadeOrchestrationService) filterStartable(order []string) ([]string, error) {
	filtered := make([]string, 0, len(order))
	for _, name := range order {
		managed, err := s.runtime.FindByName(name)
		if err != nil {
			return nil, err
		}
		if IsStartableServiceStatus(managed.Status) {
			filtered = append(filtered, name)
		}
	}
	return filtered, nil
}

func (s *ServiceCascadeOrchestrationService) filterStoppable(order []string, targetName string) ([]string, error) {
	filtered := make([]string, 0, len(order))
	for _, name := range order {
		if name == targetName {
			filtered = append(filtered, name)
			continue
		}
		managed, err := s.runtime.FindByName(name)
		if err != nil {
			return nil, err
		}
		if isCascadeStopCandidateStatus(managed.Status) {
			filtered = append(filtered, name)
		}
	}
	return filtered, nil
}

func collectUpstream(target string, index map[string]ServiceTopologyNode) map[string]struct{} {
	closure := make(map[string]struct{})
	var visit func(string)
	visit = func(name string) {
		if _, seen := closure[name]; seen {
			return
		}
		closure[name] = struct{}{}
		node, ok := index[name]
		if !ok {
			return
		}
		for _, dep := range node.DependsOn {
			visit(dep)
		}
	}
	visit(target)
	return closure
}

func collectDownstream(target string, dependents map[string][]string) map[string]struct{} {
	closure := make(map[string]struct{})
	var visit func(string)
	visit = func(name string) {
		if _, seen := closure[name]; seen {
			return
		}
		closure[name] = struct{}{}
		for _, child := range dependents[name] {
			visit(child)
		}
	}
	visit(target)
	return closure
}

func buildDependentsIndex(nodes []ServiceTopologyNode) map[string][]string {
	dependents := make(map[string][]string)
	for _, node := range nodes {
		for _, dep := range node.DependsOn {
			dependents[dep] = append(dependents[dep], node.Name)
		}
	}
	for name := range dependents {
		sort.Strings(dependents[name])
	}
	return dependents
}

func topologicalOrder(closure map[string]struct{}, index map[string]ServiceTopologyNode) ([]string, error) {
	if len(closure) == 0 {
		return nil, fmt.Errorf("topology closure is empty")
	}

	visitState := make(map[string]int, len(closure))
	order := make([]string, 0, len(closure))

	var visit func(string) error
	visit = func(name string) error {
		switch visitState[name] {
		case 1:
			return fmt.Errorf("cyclic dependency detected at service %q", name)
		case 2:
			return nil
		}
		visitState[name] = 1
		node, ok := index[name]
		if ok {
			for _, dep := range node.DependsOn {
				if _, inClosure := closure[dep]; !inClosure {
					continue
				}
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		visitState[name] = 2
		order = append(order, name)
		return nil
	}

	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func reverseNames(order []string) []string {
	reversed := make([]string, len(order))
	for i := range order {
		reversed[len(order)-1-i] = order[i]
	}
	return reversed
}
