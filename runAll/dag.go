package main

import "fmt"

type ServiceNode struct {
	Service    Service
	DependsOn  []string
	Dependents []string
	InDegree   int
}

type ExecutionLevel struct {
	Services []*ServiceNode
}

func BuildDAG(services []Service) ([]ExecutionLevel, error) {
	if len(services) == 0 {
		return nil, nil
	}

	nodes := make(map[string]*ServiceNode, len(services))
	for _, svc := range services {
		nodes[svc.Name] = &ServiceNode{
			Service:   svc,
			DependsOn: svc.DependsOn,
		}
	}

	// Compute dependents and indegrees
	for _, node := range nodes {
		for _, depName := range node.DependsOn {
			dep := nodes[depName]
			dep.Dependents = append(dep.Dependents, node.Service.Name)
		}
		node.InDegree = len(node.DependsOn)
	}

	// Kahn's algorithm
	var levels []ExecutionLevel
	processed := 0

	// First level: nodes with indegree 0
	var currentLevel []*ServiceNode
	for _, node := range nodes {
		if node.InDegree == 0 {
			currentLevel = append(currentLevel, node)
		}
	}

	for len(currentLevel) > 0 {
		levels = append(levels, ExecutionLevel{Services: currentLevel})
		processed += len(currentLevel)

		var nextLevel []*ServiceNode
		for _, node := range currentLevel {
			for _, depName := range node.Dependents {
				dep := nodes[depName]
				dep.InDegree--
				if dep.InDegree == 0 {
					nextLevel = append(nextLevel, dep)
				}
			}
		}
		currentLevel = nextLevel
	}

	if processed != len(nodes) {
		for _, node := range nodes {
			if node.InDegree > 0 {
				return nil, fmt.Errorf("cycle detected involving service %q", node.Service.Name)
			}
		}
	}

	return levels, nil
}
