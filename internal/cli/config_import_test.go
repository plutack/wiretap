package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutack/wiretap/internal/config"
)

func writeRelayClientTestFile(t *testing.T, path, clientID, token string) {
	t.Helper()
	f, err := config.NewRelayClientFile("wss://relay.example.com/tunnel", clientID, token, []string{"orders"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigImportCmd(t *testing.T) {
	base := withTempConfigManager(t)
	path := filepath.Join(t.TempDir(), "alice.wiretap-client.json")
	writeRelayClientTestFile(t, path, "client-alice", "token-alice")

	out, _, err := runCmd(t, "dev", "config", "import", path)
	if err != nil {
		t.Fatalf("config import: %v", err)
	}
	if !strings.Contains(out, "imported relay client client-alice") || !strings.Contains(out, "plaintext bearer token") {
		t.Fatalf("stdout = %q", out)
	}
	m := config.NewManager(config.WithBaseDir(base))
	cfg, _ := m.Load()
	creds, _ := m.LoadCredentials()
	if cfg.Relay.URL != "wss://relay.example.com/tunnel" || creds.ClientID != "client-alice" || creds.ClientToken != "token-alice" {
		t.Fatalf("cfg=%+v creds=%+v", cfg.Relay, creds)
	}
}

func TestConfigImportCmdRequiresForce(t *testing.T) {
	base := withTempConfigManager(t)
	m := config.NewManager(config.WithBaseDir(base))
	if err := m.SaveCredentials(config.Credentials{ClientID: "old", ClientToken: "old-token"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "new.wiretap-client.json")
	writeRelayClientTestFile(t, path, "new", "new-token")

	if _, _, err := runCmd(t, "dev", "config", "import", path); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want force guidance", err)
	}
	if _, _, err := runCmd(t, "dev", "config", "import", "--force", path); err != nil {
		t.Fatalf("forced config import: %v", err)
	}
}
