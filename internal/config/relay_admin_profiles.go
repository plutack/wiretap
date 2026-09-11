package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const relayAdminProfilesVersion = 1

// RelayAdminProfile is non-secret metadata for a previously connected relay.
// The corresponding admin token is stored in the operating system keyring.
type RelayAdminProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RelayURL string `json:"relay_url"`
	LastUsed int64  `json:"last_used"`
}

type relayAdminProfileFile struct {
	Version  int                 `json:"version"`
	Profiles []RelayAdminProfile `json:"profiles"`
}

func (m *Manager) RelayAdminProfilesPath() (string, error) {
	dir, err := m.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "relay-admin-profiles.json"), nil
}

func (m *Manager) LoadRelayAdminProfiles() ([]RelayAdminProfile, error) {
	path, err := m.RelayAdminProfilesPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []RelayAdminProfile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read relay admin profiles: %w", err)
	}
	var file relayAdminProfileFile
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("config: parse relay admin profiles: %w", err)
	}
	if file.Version != relayAdminProfilesVersion {
		return nil, fmt.Errorf("config: unsupported relay admin profiles version %d", file.Version)
	}
	if file.Profiles == nil {
		file.Profiles = []RelayAdminProfile{}
	}
	return file.Profiles, nil
}

func (m *Manager) SaveRelayAdminProfiles(profiles []RelayAdminProfile) error {
	path, err := m.RelayAdminProfilesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: create profiles dir: %w", err)
	}
	b, err := json.MarshalIndent(relayAdminProfileFile{Version: relayAdminProfilesVersion, Profiles: profiles}, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal relay admin profiles: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("config: write relay admin profiles: %w", err)
	}
	return nil
}
