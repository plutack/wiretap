package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/plutack/wiretap/internal/api"
	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/store"
)

// noopTunnel satisfies app.TunnelRunner and blocks until cancelled, standing
// in for the real relay client in settings tests (which do start tunnels).
type noopTunnel struct{}

func (noopTunnel) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// newSettingsBindings is newBindings with a tunnel factory that tolerates
// (and counts) tunnel starts instead of failing the test.
func newSettingsBindings(t *testing.T) (*Bindings, *app.App, *atomic.Int32) {
	t.Helper()
	base := t.TempDir()
	mgr := config.NewManager(config.WithBaseDir(base))
	var starts atomic.Int32
	a := app.New(mgr, app.WithTunnelFactory(func(app.TunnelConfig, *store.PCStore) app.TunnelRunner {
		starts.Add(1)
		return noopTunnel{}
	}))
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return New(a, WithVersion("test")), a, &starts
}

func TestBindings_GetSettings_Defaults(t *testing.T) {
	t.Parallel()
	b, _, _ := newSettingsBindings(t)
	v, err := b.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if v.ProxyAddr != "127.0.0.1:8888" || v.LocalAPIAddr != "127.0.0.1:9876" {
		t.Errorf("intercept defaults not reflected: %+v", v)
	}
	if v.TUITheme != "dark" {
		t.Errorf("TUITheme = %q, want dark", v.TUITheme)
	}
	if v.NativeTitlebar != "auto" {
		t.Errorf("NativeTitlebar = %q, want auto", v.NativeTitlebar)
	}
	if v.Registered {
		t.Error("Registered = true before any registration")
	}
	if v.ConfigPath == "" || v.CredsPath == "" || v.StoreDefault == "" {
		t.Errorf("resolved paths missing: %+v", v)
	}
}

func TestBindings_GetSettings_ReadsExistingConfigAndCreds(t *testing.T) {
	t.Parallel()
	b, a, _ := newSettingsBindings(t)

	// Simulate pre-existing CLI-era configuration: a config file on disk and
	// saved relay credentials.
	cfg := config.Default()
	cfg.Relay.URL = "wss://relay.example.com/tunnel"
	cfg.Intercept.ProxyAddr = "127.0.0.1:7000"
	if err := a.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := a.SaveRelayCredentials(config.Credentials{
		ClientID: "c-123", ClientToken: "secret", Projects: []string{"p1", "p2"},
	}); err != nil {
		t.Fatalf("SaveRelayCredentials: %v", err)
	}

	v, err := b.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if v.RelayURL != "wss://relay.example.com/tunnel" || v.ProxyAddr != "127.0.0.1:7000" {
		t.Errorf("existing config not reflected: %+v", v)
	}
	if !v.Registered || v.ClientID != "c-123" || len(v.Projects) != 2 {
		t.Errorf("existing registration not reflected: %+v", v)
	}
	// The secret must never reach the frontend view.
	if js, _ := json.Marshal(v); strings.Contains(string(js), "secret") {
		t.Errorf("settings view leaks the client token: %s", js)
	}
}

func TestBindings_SaveSettings_PersistsAndRestartsTunnel(t *testing.T) {
	t.Parallel()
	b, a, starts := newSettingsBindings(t)
	if err := a.SaveRelayCredentials(config.Credentials{ClientID: "c", ClientToken: "t", Projects: []string{"p"}}); err != nil {
		t.Fatalf("SaveRelayCredentials: %v", err)
	}

	v, err := b.SaveSettings(SettingsInput{
		RelayURL:     "https://relay.example.com", // http form must be converted
		TUITheme:     "dark",
		ProxyAddr:    "127.0.0.1:8888",
		LocalAPIAddr: "127.0.0.1:9876",
		Shell:        "fish",
	})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if v.RelayURL != "wss://relay.example.com/tunnel" {
		t.Errorf("RelayURL = %q, want converted tunnel URL", v.RelayURL)
	}
	if v.Shell != "fish" {
		t.Errorf("Shell = %q, want fish", v.Shell)
	}
	if starts.Load() != 1 {
		t.Errorf("tunnel starts = %d, want 1 (URL changed)", starts.Load())
	}
	if !v.TunnelRunning {
		t.Error("TunnelRunning = false after relay URL set with credentials present")
	}

	// Config file round-trips through the manager.
	mgrCfg, err := config.NewManager(config.WithBaseDir(configBase(t, a))).Load()
	if err == nil && mgrCfg.Relay.URL != "wss://relay.example.com/tunnel" {
		t.Errorf("persisted relay URL = %q", mgrCfg.Relay.URL)
	}

	// Saving again with the same relay URL must not bounce the tunnel.
	if _, err := b.SaveSettings(SettingsInput{
		RelayURL:     "wss://relay.example.com/tunnel",
		TUITheme:     "dark",
		ProxyAddr:    "127.0.0.1:8888",
		LocalAPIAddr: "127.0.0.1:9876",
	}); err != nil {
		t.Fatalf("second SaveSettings: %v", err)
	}
	if starts.Load() != 1 {
		t.Errorf("tunnel starts = %d, want still 1 (URL unchanged)", starts.Load())
	}
}

func TestBindings_SaveSettings_Validation(t *testing.T) {
	t.Parallel()
	b, _, _ := newSettingsBindings(t)
	cases := []SettingsInput{
		{RelayURL: "ftp://nope"},
		{ProxyAddr: "no-port"},
		{LocalAPIAddr: "also no port"},
		{Shell: "zsh"},
		{NativeTitlebar: "sometimes"},
	}
	for _, in := range cases {
		if _, err := b.SaveSettings(in); err == nil {
			t.Errorf("SaveSettings(%+v): expected validation error", in)
		}
	}
}

// TestBindings_SaveSettings_NativeTitlebar covers the titlebar mode
// round-trip: a valid mode persists to config.yaml, an empty input falls
// back to the default, and GetSettings reflects what is on disk.
func TestBindings_SaveSettings_NativeTitlebar(t *testing.T) {
	t.Parallel()
	b, _, _ := newSettingsBindings(t)
	var applied []string
	b.onTitlebarModeChange = func(mode string) { applied = append(applied, mode) }

	v, err := b.SaveSettings(SettingsInput{NativeTitlebar: "always"})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if v.NativeTitlebar != "always" {
		t.Errorf("NativeTitlebar = %q, want always", v.NativeTitlebar)
	}
	if len(applied) != 1 || applied[0] != "always" {
		t.Fatalf("applied modes = %v, want [always]", applied)
	}

	// Saving an unchanged titlebar mode must not touch the live window.
	if _, err := b.SaveSettings(SettingsInput{NativeTitlebar: "always"}); err != nil {
		t.Fatalf("SaveSettings unchanged: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("unchanged mode applied again: %v", applied)
	}

	// Empty means "unset": valid, and the runtime treats it as auto.
	v, err = b.SaveSettings(SettingsInput{})
	if err != nil {
		t.Fatalf("SaveSettings empty: %v", err)
	}
	if v.NativeTitlebar != "" {
		t.Errorf("NativeTitlebar = %q, want empty (default)", v.NativeTitlebar)
	}
	if len(applied) != 2 || applied[1] != "" {
		t.Fatalf("applied modes = %v, want [always empty]", applied)
	}
}

func TestBindings_RegisterRelay(t *testing.T) {
	t.Parallel()
	b, a, starts := newSettingsBindings(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/register" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var req api.RegisterRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.AdminToken != "tok" || len(req.Projects) != 2 {
			t.Errorf("unexpected register request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(api.RegisterResponse{
			ClientID: "c-1", ClientToken: "ct-1", Projects: req.Projects,
		})
	}))
	defer srv.Close()

	v, err := b.RegisterRelay(RegisterInput{
		RelayURL:    srv.URL,
		AdminToken:  "tok",
		Projects:    []string{" project-a ", "/project-b/"},
		DisplayName: "laptop",
	})
	if err != nil {
		t.Fatalf("RegisterRelay: %v", err)
	}
	if v.ClientID != "c-1" || len(v.Projects) != 2 {
		t.Errorf("RegisterView = %+v", v)
	}
	if !strings.HasPrefix(v.TunnelURL, "ws://") || !strings.HasSuffix(v.TunnelURL, "/tunnel") {
		t.Errorf("TunnelURL = %q, want ws://…/tunnel", v.TunnelURL)
	}

	// Credentials + config were persisted and the tunnel started.
	creds, err := a.RelayCredentials()
	if err != nil || creds.ClientID != "c-1" || creds.ClientToken != "ct-1" {
		t.Errorf("credentials not persisted: %+v, %v", creds, err)
	}
	cfg, _ := a.Config()
	if cfg.Relay.URL != v.TunnelURL {
		t.Errorf("config relay URL = %q, want %q", cfg.Relay.URL, v.TunnelURL)
	}
	if starts.Load() != 1 {
		t.Errorf("tunnel starts = %d, want 1", starts.Load())
	}

	// And the settings screen now reports the registration.
	sv, err := b.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if !sv.Registered || sv.ClientID != "c-1" {
		t.Errorf("settings after register = %+v", sv)
	}
}

func TestBindings_RegisterRelay_Validation(t *testing.T) {
	t.Parallel()
	b, _, _ := newSettingsBindings(t)
	cases := []RegisterInput{
		{AdminToken: "t", Projects: []string{"p"}},                                     // no URL
		{RelayURL: "https://r.example.com", Projects: []string{"p"}},                   // no token
		{RelayURL: "https://r.example.com", AdminToken: "t"},                           // no projects
		{RelayURL: "https://r.example.com", AdminToken: "t", Projects: []string{"  "}}, // blank project
	}
	for _, in := range cases {
		if _, err := b.RegisterRelay(in); err == nil {
			t.Errorf("RegisterRelay(%+v): expected validation error", in)
		}
	}
}

// configBase recovers the temp base dir behind an App's manager via its
// config path (…/wiretap/config.yaml → base).
func configBase(t *testing.T, a *app.App) string {
	t.Helper()
	p, err := a.ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	// p = <base>/wiretap/config.yaml
	return strings.TrimSuffix(p, "/wiretap/config.yaml")
}
