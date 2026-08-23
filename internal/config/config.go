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
	Relay     RelayConfig     `yaml:"relay"`
	Store     StoreConfig     `yaml:"store"`
	TUI       TUIConfig       `yaml:"tui"`
	Intercept InterceptConfig `yaml:"intercept"`
	GUI       GUIConfig       `yaml:"gui"`
}

// RelayConfig holds the outbound-tunnel settings used by relayclient.
type RelayConfig struct {
	// URL is the WebSocket endpoint of the relay, e.g.
	// "wss://relay.example.com/tunnel". Empty means the tunnel is disabled.
	URL string `yaml:"url"`
	// ForwardURL, when set, is the local URL every incoming webhook is
	// automatically POSTed to right after it is stored — the "just deliver it
	// to my dev server" mode. on_replay scripts run first, exactly like a
	// manual replay. Empty disables auto-forwarding.
	ForwardURL string `yaml:"forward_url"`
	// CredsFile is the path to the client_id/client_token JSON written by
	// `wiretap relay register`. Defaults to <config dir>/relay-credentials.json.
	CredsFile string `yaml:"creds_file"`
	// Note: the set of project paths is owned by the relay (which rejects
	// ingress to unclaimed paths) and mirrored locally in relay-credentials.json
	// (written by `wiretap relay register --save`). It is deliberately not a
	// config field — keeping it here would create a third copy that drifts.
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

// GUIConfig holds desktop dashboard preferences.
type GUIConfig struct {
	// NativeTitlebar selects who draws the window title bar. "auto" (the
	// default) marks the window frameless on desktops whose compositor
	// provides server-side decorations (COSMIC, KDE Plasma) so its native
	// bar is used instead of GTK's fallback; "always"/"never" force the
	// choice for every desktop.
	NativeTitlebar string `yaml:"native_titlebar"`
}

// InterceptConfig holds the traffic-interception settings consumed by the
// `wiretap intercept` commands: the local interception proxy listen address,
// the local 127.0.0.1 control HTTP API address, and an optional shell
// override (auto-detected from $SHELL when empty).
type InterceptConfig struct {
	// ProxyAddr is the host:port the interception proxy listens on. Clients
	// point HTTP_PROXY/HTTPS_PROXY here.
	ProxyAddr string `yaml:"proxy_addr"`
	// LocalAPIAddr is the 127.0.0.1 control HTTP API address
	// (/local/webhooks, /local/captures) external scripts can query.
	LocalAPIAddr string `yaml:"local_api_addr"`
	// Shell selects the shell kind to spawn for `wiretap intercept start`
	// (one of bash, fish, powershell, gitbash). Empty means auto-detect.
	Shell string `yaml:"shell"`
}

// Default returns the zero-touch defaults. Manager.Init writes this to
// disk; Manager.Load overlays user values on top of it.
func Default() Config {
	return Config{
		Relay: RelayConfig{
			URL:        "",
			ForwardURL: "",
			CredsFile:  "",
		},
		Store: StoreConfig{Path: ""},
		TUI:   TUIConfig{Theme: "dark"},
		GUI:   GUIConfig{NativeTitlebar: "auto"},
		Intercept: InterceptConfig{
			ProxyAddr:    "127.0.0.1:8888",
			LocalAPIAddr: "127.0.0.1:9876",
			Shell:        "",
		},
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

// Save marshals cfg and writes it to config.yaml (creating the directory
// when needed) with mode 0600, the same shape Init writes. It returns the
// path written. Used by the GUI settings screen, which edits the whole
// config in one shot; last-write-wins is acceptable for a single-user
// desktop file.
func (m *Manager) Save(cfg *Config) (string, error) {
	p, err := m.Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("config: create dir: %w", err)
	}
	b, err := yaml.Marshal(cfg)
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
