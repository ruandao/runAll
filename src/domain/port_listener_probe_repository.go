package domain

type PortListenerProbeRepository interface {
	ListListeningPIDs(port string) ([]int, error)
}
