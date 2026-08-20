package gui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/plutack/wiretap/internal/api"
	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/config"
)

// Settings bindings: the GUI settings screen edits config.yaml and performs
// relay registration without touching the CLI. The admin token is used for
// the one registration call and never persisted — storing it (ideally in the
// OS keychain) is a follow-up.

// SettingsView is the full settings payload: current config values, resolved
// paths for display, and the relay registration state (credentials minus the
// secret token).
type SettingsView struct {
	ConfigPath    string   `json:"config_path"`
	RelayURL      string   `json:"relay_url"`
	ForwardURL    string   `json:"forward_url"`
	StorePath     string   `json:"store_path"`
	StoreDefault  string   `json:"store_default"`
	TUITheme      string   `json:"tui_theme"`
	ProxyAddr     string   `json:"proxy_addr"`
	LocalAPIAddr  string   `json:"local_api_addr"`
	Shell         string   `json:"shell"`
	Registered    bool     `json:"registered"`
	ClientID      string   `json:"client_id,omitempty"`
	Projects      []string `json:"projects,omitempty"`
	CredsPath     string   `json:"creds_path"`
	TunnelRunning bool     `json:"tunnel_running"`
}

// SettingsInput is the editable subset SaveSettings writes back. Empty
// strings are meaningful ("use the default"), so the whole form is sent
// every time rather than a patch.
type SettingsInput struct {
	RelayURL     string `json:"relay_url"`
	ForwardURL   string `json:"forward_url"`
	StorePath    string `json:"store_path"`
	TUITheme     string `json:"tui_theme"`
	ProxyAddr    string `json:"proxy_addr"`
	LocalAPIAddr string `json:"local_api_addr"`
	Shell        string `json:"shell"`
}

// RegisterInput is the relay registration form: the relay URL (HTTPS base or
// wss tunnel form — both accepted), the admin token (used once, not stored),
// the project paths to claim, and an optional display name.
type RegisterInput struct {
	RelayURL    string   `json:"relay_url"`
	AdminToken  string   `json:"admin_token"`
	Projects    []string `json:"projects"`
	DisplayName string   `json:"display_name"`
}

// RegisterView reports a successful registration: the assigned client id,
// the claimed projects, and the tunnel URL written to the config.
type RegisterView struct {
	ClientID  string   `json:"client_id"`
	Projects  []string `json:"projects"`
	TunnelURL string   `json:"tunnel_url"`
}

// GetSettings returns the current configuration + registration state for the
// settings screen. Missing config/credential files are not errors — the view
// simply reflects defaults / "not registered". The config is re-read from
// disk (not served from App's cache) so the form always shows what is
// actually configured, including edits made outside the GUI while it runs.
func (b *Bindings) GetSettings() (SettingsView, error) {
	cfg, err := b.app.ReloadConfig()
	if err != nil {
		return SettingsView{}, fmt.Errorf("load config: %w", err)
	}
	v := SettingsView{
		RelayURL:      cfg.Relay.URL,
		ForwardURL:    cfg.Relay.ForwardURL,
		StorePath:     cfg.Store.Path,
		TUITheme:      cfg.TUI.Theme,
		ProxyAddr:     cfg.Intercept.ProxyAddr,
		LocalAPIAddr:  cfg.Intercept.LocalAPIAddr,
		Shell:         cfg.Intercept.Shell,
		TunnelRunning: b.app.TunnelRunning(),
	}
	if p, err := b.app.ConfigPath(); err == nil {
		v.ConfigPath = p
	}
	if p, err := b.app.CredsPath(); err == nil {
		v.CredsPath = p
	}
	if p, err := b.app.DefaultStorePath(); err == nil {
		v.StoreDefault = p
	}
	if creds, err := b.app.RelayCredentials(); err == nil && creds.ClientID != "" {
		v.Registered = true
		v.ClientID = creds.ClientID
		v.Projects = creds.Projects
	}
	return v, nil
}

// SaveSettings validates and persists the whole form to config.yaml, then
// restarts the relay tunnel when its endpoint changed. Interception settings
// apply to the next `wiretap intercept start`; a store path change takes
// effect after the app restarts (the open SQLite handle is kept).
func (b *Bindings) SaveSettings(in SettingsInput) (SettingsView, error) {
	cur, err := b.app.Config()
	if err != nil {
		return SettingsView{}, fmt.Errorf("load config: %w", err)
	}

	relayURL, err := normalizeTunnelURL(in.RelayURL)
	if err != nil {
		return SettingsView{}, err
	}
	if err := validateHTTPURL("forward URL", in.ForwardURL); err != nil {
		return SettingsView{}, err
	}
	if err := validateAddr("proxy address", in.ProxyAddr); err != nil {
		return SettingsView{}, err
	}
	if err := validateAddr("local API address", in.LocalAPIAddr); err != nil {
		return SettingsView{}, err
	}
	if err := validateShell(in.Shell); err != nil {
		return SettingsView{}, err
	}

	cfg := *cur // copy; writers never mutate the shared snapshot
	cfg.Relay.URL = relayURL
	cfg.Relay.ForwardURL = strings.TrimSpace(in.ForwardURL)
	cfg.Store.Path = strings.TrimSpace(in.StorePath)
	cfg.TUI.Theme = strings.TrimSpace(in.TUITheme)
	cfg.Intercept.ProxyAddr = strings.TrimSpace(in.ProxyAddr)
	cfg.Intercept.LocalAPIAddr = strings.TrimSpace(in.LocalAPIAddr)
	cfg.Intercept.Shell = strings.TrimSpace(in.Shell)

	if err := b.app.SaveConfig(cfg); err != nil {
		return SettingsView{}, fmt.Errorf("save config: %w", err)
	}
	if cfg.Relay.URL != cur.Relay.URL {
		if err := b.app.RestartTunnel(context.Background()); err != nil {
			return SettingsView{}, fmt.Errorf("config saved, but tunnel restart failed: %w", err)
		}
	}
	return b.GetSettings()
}

// RegisterRelay performs `wiretap relay register --save` from the GUI: it
// registers this PC with the relay's admin API, persists the returned
// credentials, points the config's tunnel endpoint at the relay, and
// (re)starts the tunnel.
func (b *Bindings) RegisterRelay(in RegisterInput) (RegisterView, error) {
	projects := make([]string, 0, len(in.Projects))
	for _, p := range in.Projects {
		if p = strings.TrimSpace(strings.Trim(strings.TrimSpace(p), "/")); p != "" {
			projects = append(projects, p)
		}
	}
	switch {
	case strings.TrimSpace(in.RelayURL) == "":
		return RegisterView{}, errors.New("register: relay URL is required")
	case strings.TrimSpace(in.AdminToken) == "":
		return RegisterView{}, errors.New("register: admin token is required")
	case len(projects) == 0:
		return RegisterView{}, errors.New("register: at least one project path is required")
	}

	tunnelURL, err := app.TunnelURLFromBase(in.RelayURL)
	if err != nil {
		return RegisterView{}, err
	}
	adminBase := app.IngressBaseURL(tunnelURL)
	if adminBase == "" {
		return RegisterView{}, fmt.Errorf("register: cannot derive admin URL from %q", in.RelayURL)
	}

	client, err := api.NewClient(adminBase, api.WithAdminToken(strings.TrimSpace(in.AdminToken)))
	if err != nil {
		return RegisterView{}, fmt.Errorf("register: %w", err)
	}
	resp, err := client.Register(context.Background(), api.RegisterRequest{
		AdminToken:  strings.TrimSpace(in.AdminToken),
		Projects:    projects,
		DisplayName: strings.TrimSpace(in.DisplayName),
	})
	if err != nil {
		return RegisterView{}, fmt.Errorf("register: %w", err)
	}

	if err := b.app.SaveRelayCredentials(config.Credentials{
		ClientID:    resp.ClientID,
		ClientToken: resp.ClientToken,
		Projects:    resp.Projects,
	}); err != nil {
		return RegisterView{}, fmt.Errorf("registered, but saving credentials failed: %w", err)
	}

	cur, err := b.app.Config()
	if err != nil {
		return RegisterView{}, fmt.Errorf("registered, but loading config failed: %w", err)
	}
	cfg := *cur
	cfg.Relay.URL = tunnelURL
	if err := b.app.SaveConfig(cfg); err != nil {
		return RegisterView{}, fmt.Errorf("registered, but saving config failed: %w", err)
	}
	if err := b.app.RestartTunnel(context.Background()); err != nil {
		return RegisterView{}, fmt.Errorf("registered, but tunnel restart failed: %w", err)
	}

	return RegisterView{ClientID: resp.ClientID, Projects: resp.Projects, TunnelURL: tunnelURL}, nil
}

// --- validation helpers ---------------------------------------------------

// normalizeTunnelURL accepts the relay endpoint in either form — empty
// (tunnel disabled), ws(s):// (stored verbatim), or http(s):// (converted to
// the wss tunnel form) — and rejects anything else.
func normalizeTunnelURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("settings: invalid relay URL %q", raw)
	}
	switch u.Scheme {
	case "ws", "wss":
		return raw, nil
	case "http", "https":
		return app.TunnelURLFromBase(raw)
	default:
		return "", fmt.Errorf("settings: relay URL must be ws(s):// or http(s)://, got %q", u.Scheme)
	}
}

// validateHTTPURL accepts an empty string or an absolute http(s) URL.
func validateHTTPURL(label, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("settings: %s must be an http(s) URL, got %q", label, raw)
	}
	return nil
}

func validateAddr(label, addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("settings: %s must be host:port: %w", label, err)
	}
	return nil
}

func validateShell(shell string) error {
	switch strings.TrimSpace(shell) {
	case "", "bash", "fish", "powershell", "gitbash":
		return nil
	default:
		return fmt.Errorf("settings: unknown shell %q (use bash, fish, powershell, or gitbash)", shell)
	}
}
