// Package config owns the on-disk configuration representation and the
// small Manager that resolves its path, creates a default file, and loads
// it back. The Manager is the only piece that does I/O, so tests pin its
// base directory via WithBaseDir and never touch the real user config.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the in-memory representation of ~/.config/wiretap/config.yaml.
// Tags are yaml so the file reads naturally when written by Manager.Init.
type Config struct {
	ListenAddr string      `yaml:"listen_addr"`
	Relay      RelayConfig `yaml:"relay"`
	Store      StoreConfig `yaml:"store"`
	TUI        TUIConfig   `yaml:"tui"`
}

// RelayConfig holds the outbound-tunnel settings used by relayclient.
type RelayConfig struct {
	// URL is the WebSocket endpoint of the relay, e.g.
	// "wss://relay.example.com/tunnel". Empty means the tunnel is disabled.
	URL string `yaml:"url"`
	// Projects is the set of project paths this PC claims on the relay.
	// At least one is required when URL is set. (Multi-project per client.)
	Projects []string `yaml:"projects"`
	// CredsFile is the path to the client_id/client_token JSON written by
	// `wiretap relay register`. Defaults to <config dir>/relay-credentials.json.
	CredsFile string `yaml:"creds_file"`
}

// StoreConfig points at the local SQLite database.
type StoreConfig struct {
	// Path to wiretap.db. Defaults to <data dir>/wiretap.db.
	Path string `yaml:"path"`
}

// TUIConfig holds Bubbletea presentation options.
type TUIConfig struct {
	Theme string `yaml:"theme"`
}

// Default returns the zero-touch defaults. Manager.Init writes this to
// disk; Manager.Load overlays user values on top of it.
func Default() Config {
	return Config{
		ListenAddr: "127.0.0.1:8888",
		Relay: RelayConfig{
			URL:       "",
			Projects:  []string{"default"},
			CredsFile: "",
		},
		Store: StoreConfig{Path: ""},
		TUI:   TUIConfig{Theme: "dark"},
	}
}

// Manager resolves the config directory and file and performs the on-disk
// operations (Init / Load). The baseDir field is the only mutable state,
// and it is only ever set via WithBaseDir — there is no package-level
// variable.
type Manager struct {
	baseDir string
}

// Option configures a Manager.
type Option func(*Manager)

// WithBaseDir overrides the directory normally derived from
// os.UserConfigDir. Tests use this with t.TempDir() to keep the real user
// config untouched.
func WithBaseDir(dir string) Option {
	return func(m *Manager) { m.baseDir = dir }
}

// NewManager returns a Manager configured by the given options.
func NewManager(opts ...Option) *Manager {
	m := &Manager{}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Dir returns the wiretap config directory. With no override it honours
// os.UserConfigDir (XDG_CONFIG_HOME on Linux, ~/Library/Application Support
// on macOS, %AppData% on Windows) and appends "wiretap".
func (m *Manager) Dir() (string, error) {
	if m.baseDir != "" {
		return filepath.Join(m.baseDir, "wiretap"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: determine user config dir: %w", err)
	}
	return filepath.Join(base, "wiretap"), nil
}

// Path returns the full path to config.yaml inside Dir.
func (m *Manager) Path() (string, error) {
	d, err := m.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.yaml"), nil
}

// Init writes Default() to config.yaml. If a file already exists it
// returns an error unless force is true. It returns the path written.
func (m *Manager) Init(force bool) (string, error) {
	p, err := m.Path()
	if err != nil {
		return "", err
	}
	if !force {
		if _, err := os.Stat(p); err == nil {
			return "", fmt.Errorf("config: %s already exists (use --force to overwrite)", p)
		}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("config: create dir: %w", err)
	}
	cfg := Default()
	b, err := yaml.Marshal(&cfg)
	if err != nil {
		return "", fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return "", fmt.Errorf("config: write %s: %w", p, err)
	}
	return p, nil
}

// Load reads config.yaml and overlays it on Default(). Missing fields keep
// their default values; this is how we stay backward-compatible as the
// schema grows.
func (m *Manager) Load() (*Config, error) {
	p, err := m.Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", p, err)
	}
	cfg := Default()
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", p, err)
	}
	return &cfg, nil
}
