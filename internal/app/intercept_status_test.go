package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/plutack/wiretap/internal/config"
)

func TestActiveInterceptSessionID(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	mgr := config.NewManager(config.WithBaseDir(base))
	dir, err := mgr.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	a := New(mgr)

	body := fmt.Sprintf(`{"pid":%d,"session_id":42}`, os.Getpid())
	if err := os.WriteFile(filepath.Join(dir, "intercept.pid"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := a.ActiveInterceptSessionID(); got != 42 {
		t.Fatalf("ActiveInterceptSessionID = %d, want 42", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "intercept.pid"), []byte("123"), 0o644); err != nil {
		t.Fatalf("WriteFile legacy: %v", err)
	}
	if got := a.ActiveInterceptSessionID(); got != 0 {
		t.Fatalf("legacy ActiveInterceptSessionID = %d, want 0", got)
	}
}
