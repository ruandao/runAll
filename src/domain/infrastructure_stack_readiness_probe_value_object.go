package domain

import "strings"

// InfrastructureStackProbeKind distinguishes HTTP vs TCP readiness checks.
type InfrastructureStackProbeKind string

const (
	InfrastructureStackProbeHTTP InfrastructureStackProbeKind = "http"
	InfrastructureStackProbeTCP  InfrastructureStackProbeKind = "tcp"
)

// InfrastructureStackReadinessProbe describes how runAll validates a dockerInfra stack.
type InfrastructureStackReadinessProbe struct {
	kind    InfrastructureStackProbeKind
	target  string
	httpURL string
	tcpAddr string
}

func NewHTTPInfrastructureStackReadinessProbe(url string) InfrastructureStackReadinessProbe {
	return InfrastructureStackReadinessProbe{
		kind:    InfrastructureStackProbeHTTP,
		target:  strings.TrimSpace(url),
		httpURL: strings.TrimSpace(url),
	}
}

func NewTCPInfrastructureStackReadinessProbe(addr string) InfrastructureStackReadinessProbe {
	addr = strings.TrimSpace(addr)
	return InfrastructureStackReadinessProbe{
		kind:    InfrastructureStackProbeTCP,
		target:  addr,
		tcpAddr: addr,
	}
}

func (p InfrastructureStackReadinessProbe) Kind() InfrastructureStackProbeKind {
	return p.kind
}

func (p InfrastructureStackReadinessProbe) Target() string {
	return p.target
}

func (p InfrastructureStackReadinessProbe) HTTPURL() string {
	return p.httpURL
}

func (p InfrastructureStackReadinessProbe) TCPAddr() string {
	return p.tcpAddr
}

func (p InfrastructureStackReadinessProbe) IsValid() bool {
	return p.target != ""
}
