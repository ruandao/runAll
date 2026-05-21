package domain

type ServiceRuntimeContextRepository interface {
	FindByName(name string) (ManagedService, error)
	Save(service ManagedService) error
	ListByGroup(groupName string) ([]ManagedService, error)
	ListAll() ([]ManagedService, error)
}
