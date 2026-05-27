package domain

import (
	"fmt"
	"strings"
)

// ServiceTopologyNode is an immutable configuration node in the runAll service DAG.
type ServiceTopologyNode struct {
	Name      string
	GroupName string
	DependsOn []string
}

func NewServiceTopologyNode(name, groupName string, dependsOn []string) (ServiceTopologyNode, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ServiceTopologyNode{}, fmt.Errorf("service topology node name is required")
	}
	deps := make([]string, 0, len(dependsOn))
	for _, dep := range dependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		deps = append(deps, dep)
	}
	return ServiceTopologyNode{
		Name:      name,
		GroupName: strings.TrimSpace(groupName),
		DependsOn: deps,
	}, nil
}
