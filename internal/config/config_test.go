package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Example demonstrating table-driven subtests + t.Parallel for independent
// assertions on a value-returning function (Default does no I/O).
func TestDefault(t *testing.T) {
	cfg := Default()
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"listen_addr", cfg.ListenAddr, "127.0.0.1:8888"},
		{"relay default project", cfg.Relay.Projects[0], "default"},
		{"tui theme", cfg.TUI.Theme, "dark"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestManager_Dir_WithBaseDir(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	m := NewManager(WithBaseDir(base))

	got, err := m.Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := filepath.Join(base, "wiretap")
	if got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestManager_Path_WithBaseDir(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	m := NewManager(WithBaseDir(base))

	got, err := m.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(base, "wiretap", "config.yaml")
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestManager_Init_CreatesFile(t *testing.T) {
	t.Parallel()
	m := NewManager(WithBaseDir(t.TempDir()))

	path, err := m.Init(false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 0600", perm)
	}
}

func TestManager_Init_ErrorsWhenExists(t *testing.T) {
	t.Parallel()
	m := NewManager(WithBaseDir(t.TempDir()))

	if _, err := m.Init(false); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if _, err := m.Init(false); err == nil {
		t.Fatal("second Init without force: expected error, got nil")
	}
	if _, err := m.Init(true); err != nil {
		t.Fatalf("third Init with force: %v", err)
	}
}

// Round-trip: Init writes Default(), Load reads it back and the values
// survive the YAML cycle.
func TestManager_Load_RoundTrip(t *testing.T) {
	t.Parallel()
	m := NewManager(WithBaseDir(t.TempDir()))
	if _, err := m.Init(false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:8888" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "127.0.0.1:8888")
	}
	if len(cfg.Relay.Projects) != 1 || cfg.Relay.Projects[0] != "default" {
		t.Errorf("Relay.Projects = %v, want [default]", cfg.Relay.Projects)
	}
}

// Missing file => error, but wrapped so callers can errors.Is / IsNotExist.
func TestManager_Load_ErrorsOnMissingFile(t *testing.T) {
	t.Parallel()
	m := NewManager(WithBaseDir(t.TempDir()))
	if _, err := m.Load(); err == nil {
		t.Fatal("Load on missing file: expected error, got nil")
	}
}
