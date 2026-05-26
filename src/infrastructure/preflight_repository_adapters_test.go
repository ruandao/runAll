package infrastructure

import "testing"

func TestLsofPortListenerProbeRepository_ListListeningPIDs_InvalidPort(t *testing.T) {
	t.Parallel()

	repo := NewLsofPortListenerProbeRepository(nil)
	if _, err := repo.ListListeningPIDs(""); err == nil {
		t.Fatal("expected empty port to fail")
	}
}

func TestSyscallForeignProcessTerminationRepository_Terminate_Empty(t *testing.T) {
	t.Parallel()

	repo := NewSyscallForeignProcessTerminationRepository()
	if err := repo.Terminate(nil); err != nil {
		t.Fatalf("empty terminate should succeed: %v", err)
	}
}

func TestRunnerOwnedProcessRegistryRepository_OwnedPIDs_EmptyWhenUnset(t *testing.T) {
	t.Parallel()

	repo := NewRunnerOwnedProcessRegistryRepository(nil)
	if len(repo.OwnedPIDs()) != 0 {
		t.Fatalf("expected empty owned set, got %v", repo.OwnedPIDs())
	}
}
