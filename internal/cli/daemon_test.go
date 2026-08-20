package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWriteReadRemovePIDFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writePIDFile(dir, 12345); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}
	pid, err := readPIDFile(dir)
	if err != nil {
		t.Fatalf("readPIDFile: %v", err)
	}
	if pid != 12345 {
		t.Errorf("pid = %d, want 12345", pid)
	}
	removePIDFile(dir)
	if _, err := readPIDFile(dir); !os.IsNotExist(err) {
		t.Errorf("read after remove: expected ENOENT, got %v", err)
	}
}

func TestReadPIDFile_RejectsGarbage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, pidFileName), []byte("not a number"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := readPIDFile(dir); err == nil {
		t.Fatal("readPIDFile: expected error, got nil")
	}
}

// processAlive uses syscall.Kill(pid, 0). We can't fake it cleanly without
// privilege games, so just smoke-test the pid arithmetic by reading what we
// wrote.
func TestPIDRoundTrip_HasSamePIDAfterParse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, pidFileName), []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer removePIDFile(dir)
	pid, err := readPIDFile(dir)
	if err != nil {
		t.Fatalf("readPIDFile: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
	if !processAlive(pid) {
		t.Errorf("processAlive(%d) = false, want true (this test process should be alive)", pid)
	}
}

func TestPIDFileContent_IsJustDigits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writePIDFile(dir, 42)
	b, _ := os.ReadFile(filepath.Join(dir, pidFileName))
	if strings.TrimSpace(string(b)) != "42" {
		t.Errorf("pid file body = %q, want %q", string(b), "42")
	}
}
