package domain

import "strings"

type ServiceStopDecision struct {
	Allow            bool
	ActiveDependents []string
}

type ServiceStopPolicyService struct {
	repository ServiceRuntimeContextRepository
}

func NewServiceStopPolicyService(repository ServiceRuntimeContextRepository) *ServiceStopPolicyService {
	return &ServiceStopPolicyService{repository: repository}
}

func (s *ServiceStopPolicyService) EvaluateStop(serviceName string) (ServiceStopDecision, error) {
	services, err := s.repository.ListAll()
	if err != nil {
		return ServiceStopDecision{}, err
	}

	var activeDependents []string
	for _, candidate := range services {
		if !dependsOn(candidate.DependsOn, serviceName) {
			continue
		}
		if isBlockingStatus(candidate.Status) {
			activeDependents = append(activeDependents, candidate.Name)
		}
	}

	return ServiceStopDecision{
		Allow:            len(activeDependents) == 0,
		ActiveDependents: activeDependents,
	}, nil
}

func dependsOn(dependsOnList []string, target string) bool {
	for _, dep := range dependsOnList {
		if strings.TrimSpace(dep) == target {
			return true
		}
	}
	return false
}

func isBlockingStatus(status string) bool {
	switch status {
	case ServiceStatusHealthy,
		ServiceStatusRetrying,
		ServiceStatusStarting,
		ServiceStatusRestarting,
		ServiceStatusBuilding:
		return true
	default:
		return false
	}
}
