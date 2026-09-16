package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRelayClientFileRoundTrip(t *testing.T) {
	t.Parallel()
	f, err := NewRelayClientFile("wss://relay.example.com/tunnel", " client-1 ", " secret ", []string{"orders", "/billing/", "orders", ""})
	if err != nil {
		t.Fatalf("NewRelayClientFile: %v", err)
	}
	raw, err := f.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := ParseRelayClientFile(raw)
	if err != nil {
		t.Fatalf("ParseRelayClientFile: %v", err)
	}
	if got.Format != RelayClientFileFormat || got.Version != 1 || got.ClientID != "client-1" || got.ClientToken != "secret" {
		t.Fatalf("parsed file = %+v", got)
	}
	if strings.Join(got.Projects, ",") != "orders,billing" {
		t.Fatalf("projects = %v", got.Projects)
	}
}

func TestParseRelayClientFileRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
	}{
		{"unknown field", `{"format":"wiretap-relay-client","version":1,"relay_url":"wss://relay.test/tunnel","client_id":"c","client_token":"t","extra":true}`},
		{"wrong version", `{"format":"wiretap-relay-client","version":2,"relay_url":"wss://relay.test/tunnel","client_id":"c","client_token":"t"}`},
		{"http URL", `{"format":"wiretap-relay-client","version":1,"relay_url":"https://relay.test","client_id":"c","client_token":"t"}`},
		{"missing token", `{"format":"wiretap-relay-client","version":1,"relay_url":"wss://relay.test/tunnel","client_id":"c"}`},
		{"multiple values", `{"format":"wiretap-relay-client","version":1,"relay_url":"wss://relay.test/tunnel","client_id":"c","client_token":"t"} {}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseRelayClientFile([]byte(tc.raw)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestManagerImportRelayClient(t *testing.T) {
	t.Parallel()
	store := &credentialSecretStore{}
	m := NewManager(WithBaseDir(t.TempDir()), WithClientSecretStore(store))
	f, _ := NewRelayClientFile("wss://relay.example.com/tunnel", "client-new", "token-new", []string{"orders"})
	if err := m.ImportRelayClient(f, false); err != nil {
		t.Fatalf("ImportRelayClient: %v", err)
	}
	cfg, _ := m.Load()
	creds, _ := m.LoadCredentials()
	if cfg.Relay.URL != f.RelayURL || creds.ClientID != f.ClientID || creds.ClientToken != f.ClientToken || creds.TokenStorage != TokenStorageKeyring {
		t.Fatalf("cfg=%+v creds=%+v", cfg.Relay, creds)
	}
	raw, _ := os.ReadFile(mustCredsPath(t, m))
	if strings.Contains(string(raw), "token-new") {
		t.Fatalf("credentials file contains imported token: %s", raw)
	}
}

func TestManagerImportRelayClientRequiresForceToReplaceIdentity(t *testing.T) {
	t.Parallel()
	m := NewManager(WithBaseDir(t.TempDir()))
	if err := m.SaveCredentials(Credentials{ClientID: "old", ClientToken: "old-token"}); err != nil {
		t.Fatal(err)
	}
	f, _ := NewRelayClientFile("wss://relay.example.com/tunnel", "new", "new-token", nil)
	if err := m.ImportRelayClient(f, false); !errors.Is(err, ErrRelayIdentityExists) {
		t.Fatalf("err = %v, want ErrRelayIdentityExists", err)
	}
	if err := m.ImportRelayClient(f, true); err != nil {
		t.Fatalf("forced import: %v", err)
	}
	creds, _ := m.LoadCredentials()
	if creds.ClientID != "new" || creds.ClientToken != "new-token" {
		t.Fatalf("credentials after force = %+v", creds)
	}
}

func TestManagerImportRelayClientCanReplaceIdentityWithUnavailableKeyring(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	writer := NewManager(WithBaseDir(base), WithClientSecretStore(&credentialSecretStore{}))
	if err := writer.SaveCredentials(Credentials{ClientID: "unreadable-old", ClientToken: "old-token"}); err != nil {
		t.Fatal(err)
	}

	// Simulate importing on a machine where the prior keyring entry cannot be
	// resolved. Identity metadata still enforces the replacement guard, and an
	// explicit force can recover by installing the new credentials.
	m := NewManager(WithBaseDir(base))
	if _, err := m.LoadCredentials(); err == nil {
		t.Fatal("LoadCredentials unexpectedly resolved an unavailable keyring token")
	}
	f, _ := NewRelayClientFile("wss://relay.example.com/tunnel", "replacement", "new-token", nil)
	if err := m.ImportRelayClient(f, false); !errors.Is(err, ErrRelayIdentityExists) {
		t.Fatalf("err = %v, want ErrRelayIdentityExists", err)
	}
	if err := m.ImportRelayClient(f, true); err != nil {
		t.Fatalf("forced import: %v", err)
	}
	creds, err := m.LoadCredentials()
	if err != nil || creds.ClientID != "replacement" || creds.ClientToken != "new-token" {
		t.Fatalf("credentials after recovery = %+v, %v", creds, err)
	}
}
