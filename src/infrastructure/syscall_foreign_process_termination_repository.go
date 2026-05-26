package infrastructure

import (
	"syscall"
	"time"
)

type SyscallForeignProcessTerminationRepository struct{}

func NewSyscallForeignProcessTerminationRepository() SyscallForeignProcessTerminationRepository {
	return SyscallForeignProcessTerminationRepository{}
}

func (SyscallForeignProcessTerminationRepository) Terminate(pids []int) error {
	if len(pids) == 0 {
		return nil
	}

	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
			return err
		}
	}

	time.Sleep(250 * time.Millisecond)

	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if err := syscall.Kill(pid, 0); err == nil {
			if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil && killErr != syscall.ESRCH {
				return killErr
			}
		}
	}
	return nil
}
