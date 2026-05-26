package domain

type ServiceRuntimePrereqProbeRepository interface {
	Probe(service ManagedService) error
}
