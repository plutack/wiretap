package config

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

type credentialSecretStore struct {
	values map[string]string
	setErr error
}

func (s *credentialSecretStore) Set(account, secret string) error {
	if s.setErr != nil {
		return s.setErr
	}
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[account] = secret
	return nil
}

func (s *credentialSecretStore) Get(account string) (string, error) {
	secret, ok := s.values[account]
	if !ok {
		return "", errors.New("missing secret")
	}
	return secret, nil
}

func (s *credentialSecretStore) Delete(account string) error {
	delete(s.values, account)
	return nil
}

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

func TestCredentialsUseKeyringWithoutWritingToken(t *testing.T) {
	store := &credentialSecretStore{}
	m := NewManager(WithBaseDir(t.TempDir()), WithClientSecretStore(store))
	if err := m.SaveCredentials(Credentials{
		ClientID: "client-42", ClientToken: "secret-token", Projects: []string{"orders"},
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	raw, err := os.ReadFile(mustCredsPath(t, m))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if containsJSONSecret(raw, "secret-token") {
		t.Fatalf("credentials file contains client token: %s", raw)
	}

	got, err := m.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got.ClientToken != "secret-token" || got.TokenStorage != TokenStorageKeyring || got.TokenKeyring == "" {
		t.Fatalf("loaded credentials = %+v", got)
	}
}

func TestCredentialsMigrateLegacyTokenToKeyring(t *testing.T) {
	base := t.TempDir()
	legacy := NewManager(WithBaseDir(base))
	if err := legacy.SaveCredentials(Credentials{ClientID: "legacy", ClientToken: "old-token"}); err != nil {
		t.Fatalf("write legacy credentials: %v", err)
	}

	store := &credentialSecretStore{}
	m := NewManager(WithBaseDir(base), WithClientSecretStore(store))
	got, err := m.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got.TokenStorage != TokenStorageKeyring || got.ClientToken != "old-token" {
		t.Fatalf("migrated credentials = %+v", got)
	}
	raw, _ := os.ReadFile(mustCredsPath(t, m))
	if containsJSONSecret(raw, "old-token") {
		t.Fatalf("legacy token remained on disk: %s", raw)
	}
}

func TestCredentialsFallBackToProtectedFile(t *testing.T) {
	store := &credentialSecretStore{setErr: errors.New("keyring unavailable")}
	m := NewManager(WithBaseDir(t.TempDir()), WithClientSecretStore(store))
	if err := m.SaveCredentials(Credentials{ClientID: "headless", ClientToken: "file-token"}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	got, err := m.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got.ClientToken != "file-token" || got.TokenStorage != TokenStorageFile || got.StorageWarning == "" {
		t.Fatalf("fallback credentials = %+v", got)
	}
}

func mustCredsPath(t *testing.T, m *Manager) string {
	t.Helper()
	p, err := m.CredsPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func containsJSONSecret(raw []byte, secret string) bool {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	return value["client_token"] == secret
}

// filepathDir is a thin wrapper to keep imports tidy.
func filepathDir(p string) string {
	return p[:len(p)-len("/relay-credentials.json")]
}
