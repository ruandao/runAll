package domain

import (
	"fmt"
	"strings"
)

type ServiceOwnershipGuardService struct {
	ownershipRepo ServiceOwnershipRepository
}

func NewServiceOwnershipGuardService(ownershipRepo ServiceOwnershipRepository) ServiceOwnershipGuardService {
	return ServiceOwnershipGuardService{
		ownershipRepo: ownershipRepo,
	}
}

func (s ServiceOwnershipGuardService) EnsureOperableBySession(
	serviceName string,
	actorSessionID string,
) error {
	name := strings.TrimSpace(serviceName)
	actor := strings.TrimSpace(actorSessionID)
	if name == "" {
		return fmt.Errorf("service name is required")
	}
	if actor == "" {
		return fmt.Errorf("actor session id is required")
	}
	ownership, err := s.ownershipRepo.FindByServiceName(name)
	if err != nil {
		return err
	}
	if ownership.ServiceName == "" {
		return nil
	}
	if !ownership.BelongsTo(actor) {
		return fmt.Errorf(
			"service %q is owned by session %q, actor session %q requires explicit takeover",
			name,
			ownership.OwnerSessionID,
			actor,
		)
	}
	return nil
}
