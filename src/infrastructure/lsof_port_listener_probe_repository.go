package infrastructure

import (
	"fmt"
	"strings"
)

type LsofPortListenerProbeRepository struct {
	listenerFn func(port string) ([]int, error)
}

func NewLsofPortListenerProbeRepository(listenerFn func(string) ([]int, error)) LsofPortListenerProbeRepository {
	return LsofPortListenerProbeRepository{listenerFn: listenerFn}
}

func (r LsofPortListenerProbeRepository) ListListeningPIDs(port string) ([]int, error) {
	normalizedPort := strings.TrimSpace(port)
	if normalizedPort == "" {
		return nil, fmt.Errorf("port is required")
	}
	if r.listenerFn == nil {
		return nil, fmt.Errorf("listener function is required")
	}
	return r.listenerFn(normalizedPort)
}
