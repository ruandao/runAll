package domain

type ServiceOwnershipRepository interface {
	FindByServiceName(serviceName string) (ServiceOwnership, error)
	Save(ownership ServiceOwnership) error
	DeleteByServiceName(serviceName string) error
	ListAll() ([]ServiceOwnership, error)
}
