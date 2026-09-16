package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials is the persisted relay client identity plus its resolved secret.
// JSON normally contains a keyring reference instead of ClientToken; the token
// field remains readable for migration and headless compatibility.
type Credentials struct {
	ClientID       string   `json:"client_id"`
	ClientToken    string   `json:"client_token,omitempty"`
	TokenKeyring   string   `json:"token_keyring,omitempty"`
	Projects       []string `json:"projects"`
	TokenStorage   string   `json:"-"`
	StorageWarning string   `json:"-"`
}

const (
	TokenStorageKeyring = "keyring"
	TokenStorageFile    = "file"
)

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

// SaveCredentials prefers the configured system keyring for the token and
// writes only identity metadata to relay-credentials.json. When no usable
// keyring exists it preserves the previous portable behavior with a 0600 file.
func (m *Manager) SaveCredentials(c Credentials) error {
	c.TokenStorage = ""
	c.StorageWarning = ""
	if c.ClientToken != "" && m.clientSecrets != nil {
		account := credentialKeyringAccount(c.ClientID)
		if err := m.clientSecrets.Set(account, c.ClientToken); err == nil {
			c.ClientToken = ""
			c.TokenKeyring = account
		} else {
			// A tunnel must remain usable on headless Linux hosts without a
			// secret service. The existing private file is the explicit
			// compatibility fallback; GetSettings reports this state.
			c.TokenKeyring = ""
		}
	}
	return m.writeCredentials(c)
}

func (m *Manager) writeCredentials(c Credentials) error {
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
	c, err := m.loadCredentialsMetadata()
	if err != nil {
		return nil, err
	}
	if c.TokenKeyring != "" {
		if m.clientSecrets == nil {
			return nil, fmt.Errorf("config: credentials reference a system keyring, but no client secret store is configured")
		}
		token, err := m.clientSecrets.Get(c.TokenKeyring)
		if err != nil {
			return nil, fmt.Errorf("config: load relay client token from keyring: %w", err)
		}
		c.ClientToken = token
		c.TokenStorage = TokenStorageKeyring
		return c, nil
	}
	c.TokenStorage = TokenStorageFile
	if c.ClientToken != "" && m.clientSecrets != nil {
		account := credentialKeyringAccount(c.ClientID)
		if err := m.clientSecrets.Set(account, c.ClientToken); err == nil {
			disk := *c
			disk.ClientToken = ""
			disk.TokenKeyring = account
			disk.TokenStorage = ""
			if err := m.writeCredentials(disk); err != nil {
				c.StorageWarning = "Token reached the system keyring, but the protected credentials file could not be migrated."
				return c, nil
			}
			c.TokenKeyring = account
			c.TokenStorage = TokenStorageKeyring
		} else {
			c.StorageWarning = "System keyring unavailable; token remains in the protected credentials file."
		}
	}
	return c, nil
}

// loadCredentialsMetadata reads the on-disk identity without resolving its
// keyring token. Import uses this so --force can recover from a missing or
// locked keyring entry while still detecting which identity it replaces.
func (m *Manager) loadCredentialsMetadata() (*Credentials, error) {
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

func credentialKeyringAccount(clientID string) string {
	sum := sha256.Sum256([]byte(clientID))
	return fmt.Sprintf("client-%x", sum[:12])
}
