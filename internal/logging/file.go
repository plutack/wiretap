package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// OpenFile creates a private append-only diagnostic sink. A bounded file is
// reset at MaxBytes on write; no backups containing old secrets remain.
// Separate process-specific files should be used by concurrent client processes.
func OpenFile(path, format, level string) (*slog.Logger, *FileSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("diagnostic output must be a regular file, not a device or symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("open diagnostic log: %w", err)
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	sink := &FileSink{file: f}
	logger, err := New(sink, format, level)
	if err != nil {
		_ = sink.Close()
		return nil, nil, err
	}
	return logger, sink, nil
}

const MaxBytes = 10 * 1024 * 1024

// FileSink serializes writes and bounds disk use to approximately MaxBytes.
// Filesystem write failures are retained for Close to report, never stderr.
type FileSink struct {
	mu      sync.Mutex
	file    *os.File
	failure error
}

func (s *FileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := s.file.Stat()
	if err == nil && info.Size()+int64(len(p)) > MaxBytes {
		err = s.file.Truncate(0)
	}
	if err != nil {
		s.failure = err
		return 0, err
	}
	n, err := s.file.Write(p)
	if err != nil {
		s.failure = err
	}
	return n, err
}
func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.file.Close()
	if s.failure != nil {
		return s.failure
	}
	return err
}
