package domain

import (
	"fmt"
	"strings"
)

const (
	ServiceFailureCodePortConflict      = "PRECHECK_PORT_CONFLICT"
	ServiceFailureCodeDependencyUnready = "PRECHECK_DEPENDENCY_UNREADY"
	ServiceFailureCodeRuntimePrereq     = "PRECHECK_RUNTIME_PREREQ_FAILED"
	ServiceFailureCodeProcessExited     = "LAUNCH_PROCESS_EXITED"
	ServiceFailureCodeReadinessTimeout  = "READINESS_TIMEOUT"
	ServiceFailureCodeBadReadiness      = "READINESS_BAD_STATUS"
)

type ServiceFailureCode struct {
	value string
}

func NewServiceFailureCode(raw string) (ServiceFailureCode, error) {
	v := strings.TrimSpace(strings.ToUpper(raw))
	switch v {
	case ServiceFailureCodePortConflict,
		ServiceFailureCodeDependencyUnready,
		ServiceFailureCodeRuntimePrereq,
		ServiceFailureCodeProcessExited,
		ServiceFailureCodeReadinessTimeout,
		ServiceFailureCodeBadReadiness:
		return ServiceFailureCode{value: v}, nil
	default:
		return ServiceFailureCode{}, fmt.Errorf("invalid service failure code: %q", raw)
	}
}

func (c ServiceFailureCode) Value() string {
	return c.value
}
