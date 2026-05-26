package infrastructure

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"runAll/src/domain"
)

func TestFileServiceOwnershipRepository_PersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ownership.json")
	repoA := NewFileServiceOwnershipRepository(path)
	repoB := NewFileServiceOwnershipRepository(path)

	ownership, err := domain.NewServiceOwnership(
		"svc-a",
		"session-a",
		1234,
		"config-a",
		"http://127.0.0.1:9999/health",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}
	if err := repoA.Save(ownership); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := repoB.FindByServiceName("svc-a")
	if err != nil {
		t.Fatalf("FindByServiceName: %v", err)
	}
	if loaded.OwnerSessionID != "session-a" {
		t.Fatalf("OwnerSessionID = %q, want session-a", loaded.OwnerSessionID)
	}
	if loaded.PID != 1234 {
		t.Fatalf("PID = %d, want 1234", loaded.PID)
	}
}

func TestFileServiceOwnershipRepository_ConcurrentSaveAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ownership.json")
	repoA := NewFileServiceOwnershipRepository(path)
	repoB := NewFileServiceOwnershipRepository(path)

	const writesPerRepo = 60
	var wg sync.WaitGroup
	wg.Add(2)

	saveBatch := func(repo *FileServiceOwnershipRepository, prefix string) {
		defer wg.Done()
		for i := 0; i < writesPerRepo; i++ {
			ownership, err := domain.NewServiceOwnership(
				fmt.Sprintf("%s-%d", prefix, i),
				fmt.Sprintf("session-%s", prefix),
				1000+i,
				"cfg",
				"http://127.0.0.1:9999/health",
				time.Now(),
			)
			if err != nil {
				t.Errorf("NewServiceOwnership(%s-%d): %v", prefix, i, err)
				return
			}
			if err := repo.Save(ownership); err != nil {
				t.Errorf("Save(%s-%d): %v", prefix, i, err)
				return
			}
		}
	}

	go saveBatch(repoA, "svc-a")
	go saveBatch(repoB, "svc-b")
	wg.Wait()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var store map[string]domain.ServiceOwnership
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatalf("ownership file should be valid JSON: %v", err)
	}

	expectedMin := writesPerRepo * 2
	if len(store) < expectedMin {
		t.Fatalf("len(store) = %d, want at least %d", len(store), expectedMin)
	}
}
