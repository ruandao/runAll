package domain

type ForeignProcessTerminationRepository interface {
	Terminate(pids []int) error
}
