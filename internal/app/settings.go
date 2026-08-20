package app

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/plutack/wiretap/internal/config"
)

// This file is the app-level surface behind the GUI settings screen: saving
// and reloading config.yaml, reading/writing relay credentials, and bouncing
// the tunnel after relay settings change. The GUI bindings stay
// marshaling-only; everything stateful funnels through here so the CLI could
// grow the same operations without duplication.

// SaveConfig persists cfg to config.yaml and swaps it in as the active
// in-memory config. Readers holding the previous *config.Config keep their
// snapshot; new Config() calls see the saved value.
func (a *App) SaveConfig(cfg config.Config) error {
	if _, err := a.mgr.Save(&cfg); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = &cfg
	a.mu.Unlock()
	return nil
}

// ReloadConfig drops the cached config and credentials and loads them fresh
// from disk. Returns the newly-active config.
func (a *App) ReloadConfig() (*config.Config, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg = nil
	a.creds = nil
	return a.configLocked()
}

// RelayCredentials returns the stored relay registration (client id/token +
// claimed projects). Returns the injected credentials when the App was built
// with WithCredentials; otherwise loads relay-credentials.json. A missing
// file surfaces as an error the caller treats as "not registered".
func (a *App) RelayCredentials() (*config.Credentials, error) {
	if a.creds != nil {
		return a.creds, nil
	}
	return a.mgr.LoadCredentials()
}

// SaveRelayCredentials persists creds to relay-credentials.json (0600) and
// makes them the active in-memory credentials so the next StartTunnel uses
// them without a reload.
func (a *App) SaveRelayCredentials(creds config.Credentials) error {
	if err := a.mgr.SaveCredentials(creds); err != nil {
		return err
	}
	a.mu.Lock()
	a.creds = &creds
	a.mu.Unlock()
	return nil
}

// CredsPath exposes the resolved relay-credentials.json path for display in
// the settings screen.
func (a *App) CredsPath() (string, error) { return a.mgr.CredsPath() }

// ConfigPath exposes the resolved config.yaml path for display in the
// settings screen.
func (a *App) ConfigPath() (string, error) { return a.mgr.Path() }

// DefaultStorePath returns the path the store falls back to when
// store.path is empty (<configDir>/wiretap.db).
func (a *App) DefaultStorePath() (string, error) {
	dir, err := a.mgr.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wiretap.db"), nil
}

// RestartTunnel stops any running tunnel and starts it again from the
// current config + credentials. Used after relay settings change. Like
// StartTunnel it is a no-op (not an error) when the relay is unconfigured.
func (a *App) RestartTunnel(ctx context.Context) error {
	a.StopTunnel()
	a.SetConnectedProjects(nil)
	return a.StartTunnel(ctx)
}

// TunnelURLFromBase derives the WebSocket tunnel endpoint from a relay's
// HTTP(S) base URL: https://relay.example.com → wss://relay.example.com/tunnel.
// The inverse of IngressBaseURL (export.go). Accepts ws/wss input unchanged
// (with /tunnel appended when missing).
func TunnelURLFromBase(base string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("app: invalid relay URL %q", base)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "ws", "wss":
		// already a tunnel scheme
	default:
		return "", fmt.Errorf("app: unsupported relay URL scheme %q", u.Scheme)
	}
	if !strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), "/tunnel") {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/tunnel"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
