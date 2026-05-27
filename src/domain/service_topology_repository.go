package domain

// ServiceTopologyRepository exposes the configured service dependency graph.
type ServiceTopologyRepository interface {
	ListAll() ([]ServiceTopologyNode, error)
}
