package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
)

const (
	RelayClientFileFormat  = "wiretap-relay-client"
	RelayClientFileVersion = 1
)

// ErrRelayIdentityExists is returned when an import would replace a different
// locally configured relay client without explicit approval.
var ErrRelayIdentityExists = errors.New("config: a different relay client identity is already configured")

// RelayClientFile is the portable, versioned handoff created by a relay
// administrator for another Wiretap desktop. ClientToken is a bearer secret;
// callers must treat the encoded file like a password.
type RelayClientFile struct {
	Format      string   `json:"format"`
	Version     int      `json:"version"`
	RelayURL    string   `json:"relay_url"`
	ClientID    string   `json:"client_id"`
	ClientToken string   `json:"client_token"`
	Projects    []string `json:"projects"`
}

// NewRelayClientFile constructs and validates a portable client handoff. The
// relay URL must already be the ws(s) tunnel endpoint stored in config.yaml.
func NewRelayClientFile(relayURL, clientID, clientToken string, projects []string) (RelayClientFile, error) {
	f := RelayClientFile{
		Format:      RelayClientFileFormat,
		Version:     RelayClientFileVersion,
		RelayURL:    strings.TrimSpace(relayURL),
		ClientID:    strings.TrimSpace(clientID),
		ClientToken: strings.TrimSpace(clientToken),
		Projects:    normalizeClientFileProjects(projects),
	}
	if err := f.Validate(); err != nil {
		return RelayClientFile{}, err
	}
	return f, nil
}

// ParseRelayClientFile decodes one strict JSON document and validates every
// field before it can reach credential storage.
func ParseRelayClientFile(raw []byte) (RelayClientFile, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f RelayClientFile
	if err := dec.Decode(&f); err != nil {
		return RelayClientFile{}, fmt.Errorf("config: parse relay client file: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return RelayClientFile{}, err
	}
	f.RelayURL = strings.TrimSpace(f.RelayURL)
	f.ClientID = strings.TrimSpace(f.ClientID)
	f.ClientToken = strings.TrimSpace(f.ClientToken)
	f.Projects = normalizeClientFileProjects(f.Projects)
	if err := f.Validate(); err != nil {
		return RelayClientFile{}, err
	}
	return f, nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return errors.New("config: relay client file contains multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("config: parse relay client file: %w", err)
	}
	return nil
}

// Marshal returns stable, human-readable JSON suitable for copying or a
// browser download.
func (f RelayClientFile) Marshal() ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(f, "", "  ")
}

// Validate checks the portable schema without contacting the relay, allowing
// credentials to be imported while the recipient is offline.
func (f RelayClientFile) Validate() error {
	switch {
	case f.Format != RelayClientFileFormat:
		return fmt.Errorf("config: unsupported relay client file format %q", f.Format)
	case f.Version != RelayClientFileVersion:
		return fmt.Errorf("config: unsupported relay client file version %d", f.Version)
	case strings.TrimSpace(f.ClientID) == "":
		return errors.New("config: relay client file is missing client_id")
	case strings.TrimSpace(f.ClientToken) == "":
		return errors.New("config: relay client file is missing client_token")
	}
	u, err := url.Parse(strings.TrimSpace(f.RelayURL))
	if err != nil || u.Host == "" || (u.Scheme != "ws" && u.Scheme != "wss") {
		return fmt.Errorf("config: relay_url must be an absolute ws(s) URL, got %q", f.RelayURL)
	}
	return nil
}

// ImportRelayClient installs a portable identity and points config.yaml at its
// tunnel. Re-importing the same client is idempotent; replacing a different
// identity requires force.
func (m *Manager) ImportRelayClient(f RelayClientFile, force bool) error {
	if err := f.Validate(); err != nil {
		return err
	}
	existing, err := m.loadCredentialsMetadata()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("config: load existing relay credentials: %w", err)
	}
	if existing != nil && existing.ClientID != "" && existing.ClientID != f.ClientID && !force {
		return fmt.Errorf("%w (existing %s, imported %s; use --force to replace it)",
			ErrRelayIdentityExists, existing.ClientID, f.ClientID)
	}

	cfg, err := m.Load()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("config: load configuration for relay import: %w", err)
		}
		def := Default()
		cfg = &def
	}
	previousURL := cfg.Relay.URL
	cfg.Relay.URL = strings.TrimSpace(f.RelayURL)
	if _, err := m.Save(cfg); err != nil {
		return fmt.Errorf("config: save imported relay URL: %w", err)
	}
	if err := m.SaveCredentials(Credentials{
		ClientID: f.ClientID, ClientToken: f.ClientToken, Projects: slices.Clone(f.Projects),
	}); err != nil {
		cfg.Relay.URL = previousURL
		_, _ = m.Save(cfg)
		return fmt.Errorf("config: save imported relay credentials: %w", err)
	}
	if existing != nil && existing.ClientID != "" && existing.ClientID != f.ClientID && existing.TokenKeyring != "" && m.clientSecrets != nil {
		_ = m.clientSecrets.Delete(existing.TokenKeyring)
	}
	return nil
}

func normalizeClientFileProjects(projects []string) []string {
	out := make([]string, 0, len(projects))
	seen := make(map[string]struct{}, len(projects))
	for _, project := range projects {
		project = strings.Trim(strings.TrimSpace(project), "/")
		if project == "" {
			continue
		}
		if _, ok := seen[project]; ok {
			continue
		}
		seen[project] = struct{}{}
		out = append(out, project)
	}
	return out
}
