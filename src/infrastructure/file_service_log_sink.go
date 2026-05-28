package infrastructure

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FileServiceLogSink struct {
	mu      sync.Mutex
	rootDir string
	files   map[string]*os.File
}

func NewFileServiceLogSink(rootDir string) (*FileServiceLogSink, error) {
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return nil, fmt.Errorf("log root directory is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create log root: %w", err)
	}
	return &FileServiceLogSink{
		rootDir: root,
		files:   make(map[string]*os.File),
	}, nil
}

func (s *FileServiceLogSink) AppendLine(serviceName, stream, message string) error {
	if s == nil {
		return fmt.Errorf("file log sink is nil")
	}
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return fmt.Errorf("service name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.fileForServiceLocked(serviceName)
	if err != nil {
		return err
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	line := fmt.Sprintf("%s (%s) %s\n", ts, stream, message)
	_, err = file.WriteString(line)
	return err
}

func (s *FileServiceLogSink) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error
	for name, file := range s.files {
		if err := file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(s.files, name)
	}
	return firstErr
}

func (s *FileServiceLogSink) fileForServiceLocked(serviceName string) (*os.File, error) {
	if file, ok := s.files[serviceName]; ok {
		return file, nil
	}
	path := filepath.Join(s.rootDir, serviceName+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", path, err)
	}
	s.files[serviceName] = file
	return file, nil
}
