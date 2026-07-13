package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials is the on-disk representation of
// ~/.config/wiretap/relay-credentials.json. Written by `wiretap relay
// register --save` and read by the local app on startup to populate
// relayclient.Config before dialing the relay.
type Credentials struct {
	ClientID    string   `json:"client_id"`
	ClientToken string   `json:"client_token"`
	Projects    []string `json:"projects"`
}

// CredsPath returns the full path to relay-credentials.json inside the
// wiretap config directory. Uses the same Manager base-dir resolution as
// config.yaml so tests that override WithBaseDir get isolated credential
// files too.
func (m *Manager) CredsPath() (string, error) {
	d, err := m.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "relay-credentials.json"), nil
}

// SaveCredentials writes c to relay-credentials.json with mode 0600. The
// token is a secret; the restrictive mode prevents other users on the
// machine from reading it. Returns an error if the directory cannot be
// created or the file written.
func (m *Manager) SaveCredentials(c Credentials) error {
	p, err := m.CredsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("config: create creds dir: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal credentials: %w", err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", p, err)
	}
	return nil
}

// LoadCredentials reads relay-credentials.json. Returns a wrapped error
// (including the path) when the file is missing — callers use errors.Is
// with os.IsNotExist to distinguish "not registered yet" from a real I/O
// failure.
func (m *Manager) LoadCredentials() (*Credentials, error) {
	p, err := m.CredsPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", p, err)
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", p, err)
	}
	return &c, nil
}
