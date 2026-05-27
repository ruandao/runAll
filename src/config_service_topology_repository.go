package main

import "runAll/src/domain"

type configServiceTopologyRepository struct {
	cfg *Config
}

func newConfigServiceTopologyRepository(cfg *Config) *configServiceTopologyRepository {
	return &configServiceTopologyRepository{cfg: cfg}
}

func (r *configServiceTopologyRepository) ListAll() ([]domain.ServiceTopologyNode, error) {
	if r.cfg == nil {
		return nil, nil
	}
	var nodes []domain.ServiceTopologyNode
	for _, group := range r.cfg.Groups {
		for _, svc := range group.Services {
			node, err := domain.NewServiceTopologyNode(svc.Name, group.Name, svc.DependsOn)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}
