package domain

import "strings"

// CascadeStepActorResolver picks the effective session for one cascade plan step.
// When the initiating actor does not own the service, the registered owner is
// used so cascade stop/start can delegate without explicit takeover.
type CascadeStepActorResolver struct{}

func NewCascadeStepActorResolver() CascadeStepActorResolver {
	return CascadeStepActorResolver{}
}

func (CascadeStepActorResolver) Resolve(initiatingActor string, ownership ServiceOwnership, hasOwnership bool) string {
	initiating := strings.TrimSpace(initiatingActor)
	if initiating == "" {
		return initiating
	}
	if !hasOwnership || ownership.ServiceName == "" {
		return initiating
	}
	if ownership.BelongsTo(initiating) {
		return initiating
	}
	return ownership.OwnerSessionID
}
