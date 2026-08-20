package app

import (
	"testing"

	"github.com/plutack/wiretap/internal/config"
)

func TestApp_SaveAndReloadConfig(t *testing.T) {
	t.Parallel()
	a, _ := newTestApp(t)

	cfg := config.Default()
	cfg.Relay.URL = "wss://relay.example.com/tunnel"
	cfg.Intercept.ProxyAddr = "127.0.0.1:7777"
	if err := a.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// The in-memory config is swapped immediately…
	got, err := a.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if got.Relay.URL != cfg.Relay.URL || got.Intercept.ProxyAddr != "127.0.0.1:7777" {
		t.Errorf("Config after save = %+v", got)
	}

	// …and survives a reload from disk.
	reloaded, err := a.ReloadConfig()
	if err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}
	if reloaded.Relay.URL != cfg.Relay.URL {
		t.Errorf("reloaded relay URL = %q, want %q", reloaded.Relay.URL, cfg.Relay.URL)
	}
}

func TestApp_SaveRelayCredentials(t *testing.T) {
	t.Parallel()
	a, _ := newTestApp(t)

	if _, err := a.RelayCredentials(); err == nil {
		t.Fatal("expected error before registration")
	}

	creds := config.Credentials{ClientID: "c1", ClientToken: "tok", Projects: []string{"p"}}
	if err := a.SaveRelayCredentials(creds); err != nil {
		t.Fatalf("SaveRelayCredentials: %v", err)
	}
	got, err := a.RelayCredentials()
	if err != nil {
		t.Fatalf("RelayCredentials: %v", err)
	}
	if got.ClientID != "c1" || got.ClientToken != "tok" {
		t.Errorf("credentials = %+v", got)
	}
}

func TestTunnelURLFromBase(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "https://relay.example.com", want: "wss://relay.example.com/tunnel"},
		{in: "http://localhost:9000", want: "ws://localhost:9000/tunnel"},
		{in: "https://relay.example.com/base/", want: "wss://relay.example.com/base/tunnel"},
		{in: "wss://relay.example.com/tunnel", want: "wss://relay.example.com/tunnel"},
		{in: "ftp://x", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range cases {
		got, err := TunnelURLFromBase(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("TunnelURLFromBase(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("TunnelURLFromBase(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("TunnelURLFromBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
