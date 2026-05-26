package domain

import (
	"strings"
	"testing"
)

func TestFilterForeignPIDs_ExcludesOwnedAndDedupes(t *testing.T) {
	t.Parallel()

	owned := map[int]struct{}{
		100: {},
	}
	listening := []int{100, 200, 200, 0, -1, 300}

	got := FilterForeignPIDs(listening, owned)
	want := []int{200, 300}
	if len(got) != len(want) {
		t.Fatalf("FilterForeignPIDs() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FilterForeignPIDs()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestNewPortConflictSnapshot_ValidatesInput(t *testing.T) {
	t.Parallel()

	if _, err := NewPortConflictSnapshot("", "8002", []int{1}); err == nil {
		t.Fatal("expected empty service name to fail")
	}
	if _, err := NewPortConflictSnapshot("git-oauth", "", []int{1}); err == nil {
		t.Fatal("expected empty port to fail")
	}
	if _, err := NewPortConflictSnapshot("git-oauth", "8002", nil); err == nil {
		t.Fatal("expected empty foreign pids to fail")
	}

	snapshot, err := NewPortConflictSnapshot("git-oauth", "8002", []int{42, 42, 7})
	if err != nil {
		t.Fatalf("NewPortConflictSnapshot: %v", err)
	}
	if snapshot.ServiceName != "git-oauth" || snapshot.Port != "8002" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if len(snapshot.ForeignPIDs) != 2 || snapshot.ForeignPIDs[0] != 7 || snapshot.ForeignPIDs[1] != 42 {
		t.Fatalf("unexpected foreign pids: %v", snapshot.ForeignPIDs)
	}
}

func TestPortConflictCleanupResult_Succeeded(t *testing.T) {
	t.Parallel()

	okResult, err := NewPortConflictCleanupResult("git-oauth", "8002", []int{42}, nil)
	if err != nil {
		t.Fatalf("NewPortConflictCleanupResult(ok): %v", err)
	}
	if !okResult.Succeeded() {
		t.Fatal("expected cleanup to succeed when no remaining foreign pids")
	}

	failResult, err := NewPortConflictCleanupResult("git-oauth", "8002", []int{42}, []int{99})
	if err != nil {
		t.Fatalf("NewPortConflictCleanupResult(fail): %v", err)
	}
	if failResult.Succeeded() {
		t.Fatal("expected cleanup to fail when remaining foreign pids exist")
	}
}

func TestFailureHint_RenderIncludesPortAndPID(t *testing.T) {
	t.Parallel()

	hint, err := NewFailureHint(
		ServiceFailureCodePortConflict,
		"git-oauth",
		"8002",
		[]int{42, 7},
		"PRECHECK_PORT_CONFLICT: cleanup required",
	)
	if err != nil {
		t.Fatalf("NewFailureHint: %v", err)
	}

	rendered := hint.Render()
	if rendered == "" {
		t.Fatal("expected non-empty rendered hint")
	}
	for _, part := range []string{"port=8002", "pid=7,42", "PRECHECK_PORT_CONFLICT"} {
		if !strings.Contains(rendered, part) {
			t.Fatalf("rendered hint %q missing %q", rendered, part)
		}
	}
}
