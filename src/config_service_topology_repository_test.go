package main

import "testing"

func TestConfigServiceTopologyRepository_ListAll(t *testing.T) {
	cfg := &Config{
		Version: "1",
		Groups: []Group{
			{
				Name: "g1",
				Services: []Service{
					{Name: "a", Command: "true", HealthCheck: HealthCheck{URL: "http://127.0.0.1:1"}},
					{Name: "b", Command: "true", DependsOn: []string{"a"}, HealthCheck: HealthCheck{URL: "http://127.0.0.1:1"}},
				},
			},
		},
	}
	repo := newConfigServiceTopologyRepository(cfg)
	nodes, err := repo.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2", len(nodes))
	}
	if nodes[0].Name != "a" || nodes[0].GroupName != "g1" {
		t.Fatalf("node[0] = %#v", nodes[0])
	}
	if len(nodes[1].DependsOn) != 1 || nodes[1].DependsOn[0] != "a" {
		t.Fatalf("node[1] = %#v", nodes[1])
	}
}
