package logging

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFileSinkPrivateBoundedConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "test.log")
	logger, sink, err := OpenFile(path, "json", "debug")
	if err != nil {
		t.Fatal(err)
	}
	if FromContext(WithContext(context.Background(), logger)) != logger {
		t.Fatal("lost logger")
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 20 {
				logger.Info("concurrent_event", "token", "hidden")
			}
		})
	}
	wg.Wait()
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.PathSeparator != '\\' && info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "hidden") {
		t.Fatal("secret leaked")
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxBytes); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	logger, sink, err = OpenFile(path, "text", "info")
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("after_limit")
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	if info.Size() > 1024 {
		t.Fatal("file not bounded", info.Size())
	}
}

func TestFileSinkRejectsNonRegularOutput(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := OpenFile(dir, "text", "info"); err == nil {
		t.Fatal("accepted directory")
	}
	if os.PathSeparator != '\\' {
		path := filepath.Join(dir, "terminal-link")
		if err := os.Symlink("/dev/stderr", path); err != nil {
			t.Fatal(err)
		}
		if _, _, err := OpenFile(path, "text", "info"); err == nil {
			t.Fatal("accepted terminal symlink")
		}
	}
}
