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
	if err := writePIDFile(dir, 12345, 67); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}
	pid, err := readPIDFile(dir)
	if err != nil {
		t.Fatalf("readPIDFile: %v", err)
	}
	if pid != 12345 {
		t.Errorf("pid = %d, want 12345", pid)
	}
	record, err := readPIDRecord(dir)
	if err != nil || record.SessionID != 67 {
		t.Errorf("record = %+v, err = %v", record, err)
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

func TestPIDFileContent_IncludesSessionID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writePIDFile(dir, 42, 9)
	b, _ := os.ReadFile(filepath.Join(dir, pidFileName))
	if !strings.Contains(string(b), `"pid":42`) || !strings.Contains(string(b), `"session_id":9`) {
		t.Errorf("pid file body = %q", string(b))
	}
}
