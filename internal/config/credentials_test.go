package config

import (
	"os"
	"testing"
)

func TestCredentials_SaveAndLoad_RoundTrip(t *testing.T) {
	m := NewManager(WithBaseDir(t.TempDir()))

	in := Credentials{
		ClientID:    "client-42",
		ClientToken: "secret-token",
		Projects:    []string{"alpha", "beta"},
	}
	if err := m.SaveCredentials(in); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	// File must be 0600 to protect the token.
	p, _ := m.CredsPath()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 0600", perm)
	}

	out, err := m.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if out.ClientID != "client-42" || out.ClientToken != "secret-token" {
		t.Errorf("loaded = %+v", out)
	}
	if len(out.Projects) != 2 || out.Projects[0] != "alpha" {
		t.Errorf("Projects = %v", out.Projects)
	}
}

func TestLoadCredentials_MissingFile(t *testing.T) {
	m := NewManager(WithBaseDir(t.TempDir()))
	if _, err := m.LoadCredentials(); err == nil {
		t.Fatal("expected error on missing credentials file, got nil")
	}
}

func TestLoadCredentials_InvalidJSON(t *testing.T) {
	m := NewManager(WithBaseDir(t.TempDir()))
	p, _ := m.CredsPath()
	_ = os.MkdirAll(filepathDir(p), 0o755)
	_ = os.WriteFile(p, []byte("{not-json}"), 0o600)
	if _, err := m.LoadCredentials(); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestCredsPath_WithinConfigDir(t *testing.T) {
	m := NewManager(WithBaseDir(t.TempDir()))
	dir, _ := m.Dir()
	creds, _ := m.CredsPath()
	if creds != dir+"/relay-credentials.json" {
		t.Errorf("CredsPath = %q, want %q", creds, dir+"/relay-credentials.json")
	}
}

// filepathDir is a thin wrapper to keep imports tidy.
func filepathDir(p string) string {
	return p[:len(p)-len("/relay-credentials.json")]
}
