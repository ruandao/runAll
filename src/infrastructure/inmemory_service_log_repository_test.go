package infrastructure

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"runAll/src/domain"
)

func mustLogEntry(t *testing.T, service, stream, message string) domain.LogEntry {
	t.Helper()
	entry, err := domain.NewLogEntry(time.Now(), service, stream, message)
	if err != nil {
		t.Fatalf("new log entry: %v", err)
	}
	return entry
}

func TestInmemoryServiceLogRepository_AppendTail(t *testing.T) {
	repo := NewInMemoryServiceLogRepository(10)

	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, "line-1"))
	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStderr, "line-2"))
	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, "line-3"))

	logs := repo.Tail("svc-a", 2)
	if len(logs) != 2 {
		t.Fatalf("len(logs) = %d, want 2", len(logs))
	}
	if logs[0].Message != "line-2" || logs[1].Message != "line-3" {
		t.Fatalf("tail order mismatch: %#v", logs)
	}
}

func TestInmemoryServiceLogRepository_ServiceIsolation(t *testing.T) {
	repo := NewInMemoryServiceLogRepository(10)
	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, "a-1"))
	repo.Append("svc-b", mustLogEntry(t, "svc-b", domain.StreamStdout, "b-1"))

	aLogs := repo.Tail("svc-a", 10)
	bLogs := repo.Tail("svc-b", 10)
	if len(aLogs) != 1 || aLogs[0].Message != "a-1" {
		t.Fatalf("svc-a logs mismatch: %#v", aLogs)
	}
	if len(bLogs) != 1 || bLogs[0].Message != "b-1" {
		t.Fatalf("svc-b logs mismatch: %#v", bLogs)
	}
}

func TestInmemoryServiceLogRepository_CapacityTruncation(t *testing.T) {
	repo := NewInMemoryServiceLogRepository(3)
	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, "line-1"))
	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, "line-2"))
	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, "line-3"))
	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, "line-4"))

	logs := repo.Tail("svc-a", 10)
	if len(logs) != 3 {
		t.Fatalf("len(logs) = %d, want 3", len(logs))
	}
	if logs[0].Message != "line-2" || logs[2].Message != "line-4" {
		t.Fatalf("truncation mismatch: %#v", logs)
	}
}

func TestInmemoryServiceLogRepository_ConcurrentAppendSafe(t *testing.T) {
	repo := NewInMemoryServiceLogRepository(1000)
	const workers = 20
	const perWorker = 100

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(worker int) {
			defer wg.Done()
			for j := range perWorker {
				msg := fmt.Sprintf("w-%d-%d", worker, j)
				repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, msg))
			}
		}(i)
	}
	wg.Wait()

	logs := repo.Tail("svc-a", workers*perWorker)
	if len(logs) != 1000 {
		t.Fatalf("len(logs) = %d, want capped 1000", len(logs))
	}
}

func TestInmemoryServiceLogRepository_ConcurrentAppendRespectsSmallCapacity(t *testing.T) {
	repo := NewInMemoryServiceLogRepository(3)
	const workers = 8
	const perWorker = 20

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				msg := fmt.Sprintf("w-%d-%d", worker, j)
				repo.Append("svc-cap", mustLogEntry(t, "svc-cap", domain.StreamStdout, msg))
			}
		}(i)
	}
	wg.Wait()

	logs := repo.Tail("svc-cap", 100)
	if len(logs) != 3 {
		t.Fatalf("len(logs) = %d, want 3", len(logs))
	}
	for _, entry := range logs {
		if entry.ServiceName != "svc-cap" {
			t.Fatalf("entry service = %q, want svc-cap", entry.ServiceName)
		}
	}
}

func TestInmemoryServiceLogRepository_TailReturnsCopy(t *testing.T) {
	repo := NewInMemoryServiceLogRepository(10)
	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, "line-1"))
	repo.Append("svc-a", mustLogEntry(t, "svc-a", domain.StreamStdout, "line-2"))

	first := repo.Tail("svc-a", 10)
	if len(first) != 2 {
		t.Fatalf("len(first) = %d, want 2", len(first))
	}
	first[0].Message = "mutated"

	second := repo.Tail("svc-a", 10)
	if second[0].Message != "line-1" {
		t.Fatalf("tail should return defensive copy, got %#v", second)
	}
}
