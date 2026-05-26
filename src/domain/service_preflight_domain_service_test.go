package domain

import (
	"strings"
	"testing"
	"time"
)

type stubPortListenerProbeRepository struct {
	byPort map[string][]int
}

func (s *stubPortListenerProbeRepository) ListListeningPIDs(port string) ([]int, error) {
	return append([]int(nil), s.byPort[port]...), nil
}

func (s *stubPortListenerProbeRepository) removePIDs(port string, pids []int) {
	current := s.byPort[port]
	remove := make(map[int]struct{}, len(pids))
	for _, pid := range pids {
		remove[pid] = struct{}{}
	}
	remaining := make([]int, 0, len(current))
	for _, pid := range current {
		if _, drop := remove[pid]; drop {
			continue
		}
		remaining = append(remaining, pid)
	}
	s.byPort[port] = remaining
}

type stubForeignProcessTerminationRepository struct {
	probe      *stubPortListenerProbeRepository
	terminated []int
}

func (s *stubForeignProcessTerminationRepository) Terminate(pids []int) error {
	s.terminated = append(s.terminated, pids...)
	if s.probe != nil {
		for port, listeners := range s.probe.byPort {
			s.probe.removePIDs(port, pids)
			_ = listeners
		}
	}
	return nil
}

type stubOwnedProcessRegistryRepository struct {
	owned map[int]struct{}
}

func (s stubOwnedProcessRegistryRepository) OwnedPIDs() map[int]struct{} {
	return s.owned
}

func TestServicePreflightDomainService_EnsurePortsReadyForLaunch_CleansForeignPID(t *testing.T) {
	t.Parallel()

	probe := &stubPortListenerProbeRepository{
		byPort: map[string][]int{
			"8002": {4242},
		},
	}
	terminator := &stubForeignProcessTerminationRepository{probe: probe}
	owned := stubOwnedProcessRegistryRepository{owned: map[int]struct{}{}}

	service := NewServicePreflightDomainService(probe, terminator, owned)
	results, err := service.EnsurePortsReadyForLaunch("git-oauth", []string{"8002"}, true)
	if err != nil {
		t.Fatalf("EnsurePortsReadyForLaunch: %v", err)
	}
	if len(results) != 1 || !results[0].Succeeded() {
		t.Fatalf("unexpected results: %+v", results)
	}
	if len(terminator.terminated) != 1 || terminator.terminated[0] != 4242 {
		t.Fatalf("unexpected terminated pids: %v", terminator.terminated)
	}
}

func TestServicePreflightDomainService_EnsurePortsReadyForLaunch_RejectsWithoutAutoCleanup(t *testing.T) {
	t.Parallel()

	probe := &stubPortListenerProbeRepository{
		byPort: map[string][]int{
			"8002": {4242},
		},
	}
	service := NewServicePreflightDomainService(
		probe,
		&stubForeignProcessTerminationRepository{probe: probe},
		stubOwnedProcessRegistryRepository{owned: map[int]struct{}{}},
	)

	_, err := service.EnsurePortsReadyForLaunch("git-oauth", []string{"8002"}, false)
	if err == nil {
		t.Fatal("expected error when auto cleanup disabled")
	}
	if !strings.Contains(err.Error(), ServiceFailureCodePortConflict) {
		t.Fatalf("expected port conflict code in error, got: %v", err)
	}
}

func TestNewServiceLogClipboardSnapshot_PlainText(t *testing.T) {
	t.Parallel()

	entry, err := NewLogEntry(
		time.Date(2026, 5, 26, 23, 11, 39, 0, time.UTC),
		"git-oauth",
		StreamStderr,
		"Address already in use",
	)
	if err != nil {
		t.Fatalf("NewLogEntry: %v", err)
	}

	snapshot, err := NewServiceLogClipboardSnapshot("git-oauth", []LogEntry{entry})
	if err != nil {
		t.Fatalf("NewServiceLogClipboardSnapshot: %v", err)
	}

	text := snapshot.PlainText()
	if !strings.Contains(text, "(stderr) Address already in use") {
		t.Fatalf("unexpected plain text: %q", text)
	}
}
