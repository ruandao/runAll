package infrastructure

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"runAll/src/domain"
)

type FileServiceOwnershipRepository struct {
	path string
	mu   sync.Mutex
}

func NewFileServiceOwnershipRepository(path string) *FileServiceOwnershipRepository {
	return &FileServiceOwnershipRepository{
		path: strings.TrimSpace(path),
	}
}

func (r *FileServiceOwnershipRepository) FindByServiceName(serviceName string) (domain.ServiceOwnership, error) {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return domain.ServiceOwnership{}, fmt.Errorf("service name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var result domain.ServiceOwnership
	err := r.withFileLock(func() error {
		store, err := r.readStore()
		if err != nil {
			return err
		}
		ownership, exists := store[name]
		if !exists {
			return nil
		}
		result = ownership
		return nil
	})
	if err != nil {
		return domain.ServiceOwnership{}, err
	}
	return result, nil
}

func (r *FileServiceOwnershipRepository) Save(ownership domain.ServiceOwnership) error {
	name := strings.TrimSpace(ownership.ServiceName)
	if name == "" {
		return fmt.Errorf("service name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.withFileLock(func() error {
		store, err := r.readStore()
		if err != nil {
			return err
		}
		store[name] = ownership
		return r.writeStore(store)
	})
}

func (r *FileServiceOwnershipRepository) DeleteByServiceName(serviceName string) error {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return fmt.Errorf("service name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.withFileLock(func() error {
		store, err := r.readStore()
		if err != nil {
			return err
		}
		delete(store, name)
		return r.writeStore(store)
	})
}

func (r *FileServiceOwnershipRepository) ListAll() ([]domain.ServiceOwnership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result []domain.ServiceOwnership
	err := r.withFileLock(func() error {
		store, err := r.readStore()
		if err != nil {
			return err
		}
		result = make([]domain.ServiceOwnership, 0, len(store))
		for _, ownership := range store {
			result = append(result, ownership)
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].ServiceName < result[j].ServiceName
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *FileServiceOwnershipRepository) withFileLock(fn func() error) error {
	if r.path == "" {
		return fmt.Errorf("ownership file path is required")
	}
	lockPath := r.path + ".lock"
	lockDir := filepath.Dir(lockPath)
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return fmt.Errorf("create ownership lock dir: %w", err)
	}

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open ownership lock file: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire ownership lock: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	return fn()
}

func (r *FileServiceOwnershipRepository) readStore() (map[string]domain.ServiceOwnership, error) {
	store := make(map[string]domain.ServiceOwnership)
	if r.path == "" {
		return store, fmt.Errorf("ownership file path is required")
	}

	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, fmt.Errorf("read ownership store: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse ownership store: %w", err)
	}
	return store, nil
}

func (r *FileServiceOwnershipRepository) writeStore(store map[string]domain.ServiceOwnership) error {
	if r.path == "" {
		return fmt.Errorf("ownership file path is required")
	}
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create ownership dir: %w", err)
	}

	content, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ownership store: %w", err)
	}

	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o644); err != nil {
		return fmt.Errorf("write ownership temp store: %w", err)
	}
	if err := os.Rename(tmpPath, r.path); err != nil {
		return fmt.Errorf("replace ownership store: %w", err)
	}
	return nil
}
