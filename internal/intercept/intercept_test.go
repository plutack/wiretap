package intercept

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/intercept/castore"
	"github.com/plutack/wiretap/internal/intercept/shellscript"
	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/testutil"
)

// fakeInstaller is an in-memory castore.Installer for the integration test:
// it mints a real CA via castore.GenerateCA (so leaf signing works end to end),
// caches it, and tracks install/uninstall. It never touches the trust store.
type fakeInstaller struct {
	ca          *castore.CA
	ensured     int
	uninstalled int
}

func (f *fakeInstaller) EnsureCA(context.Context) (*castore.CA, error) {
	f.ensured++
	if f.ca == nil {
		ca, err := castore.GenerateCA(time.Now(), rand.Reader)
		if err != nil {
			return nil, err
		}
		f.ca = ca
	}
	return f.ca, nil
}

func (f *fakeInstaller) Uninstall(context.Context) error {
	f.uninstalled++
	return nil
}

// --- pure helper tests --------------------------------------------------

func TestGuardBlock(t *testing.T) {
	t.Parallel()
	env := shellscript.Env{
		ProxyAddr:       "127.0.0.1:8888",
		OverrideBinPath: "/tmp/wt",
		CACertPath:      "/tmp/wt/ca.crt",
	}
	tests := []struct {
		name       string
		kind       shellscript.ShellKind
		openGuard  string
		closeGuard string
	}{
		{"bash", shellscript.ShellBash, "if [ -n \"$WIRETAP_ACTIVE\" ]; then", "fi"},
		{"gitbash", shellscript.ShellGitBash, "if [ -n \"$WIRETAP_ACTIVE\" ]; then", "fi"},
		{"fish", shellscript.ShellFish, "if test -n \"$WIRETAP_ACTIVE\"", "end"},
		{"powershell", shellscript.ShellPowerShell, "if ($Env:WIRETAP_ACTIVE) {", "}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := GuardBlock(tc.kind, env)
			if err != nil {
				t.Fatalf("GuardBlock: %v", err)
			}
			if !strings.Contains(got, shellscript.SectionStart) {
				t.Errorf("missing start marker")
			}
			if !strings.Contains(got, shellscript.SectionEnd) {
				t.Errorf("missing end marker")
			}
			if !strings.Contains(got, tc.openGuard) {
				t.Errorf("missing open guard %q in:\n%s", tc.openGuard, got)
			}
			if !strings.Contains(got, tc.closeGuard) {
				t.Errorf("missing close guard %q in:\n%s", tc.closeGuard, got)
			}
			// Inner content lands too.
			if !strings.Contains(got, "HTTP_PROXY") {
				t.Errorf("inner exports missing in:\n%s", got)
			}
		})
	}
}

func TestGuardBlock_UnsupportedKind(t *testing.T) {
	t.Parallel()
	_, err := GuardBlock("ksh", shellscript.Env{})
	if err == nil {
		t.Fatal("want error for unsupported kind, got nil")
	}
	if !errors.Is(err, shellscript.ErrUnsupportedShell) {
		t.Errorf("err = %v, want wraps ErrUnsupportedShell", err)
	}
}

func TestRemoveStartupBlock(t *testing.T) {
	t.Parallel()
	pre := "# user bashrc\nexport FOO=bar\n\n"
	block, _ := GuardBlock(shellscript.ShellBash, shellscript.Env{
		ProxyAddr: "127.0.0.1:8888", OverrideBinPath: "/x", CACertPath: "/x/ca",
	})
	t.Run("removes a wrapped block", func(t *testing.T) {
		content := pre + block + "alias ll='ls -la'\n"
		got := RemoveStartupBlock(content)
		if strings.Contains(got, shellscript.SectionStart) {
			t.Errorf("start marker still present:\n%s", got)
		}
		if !strings.Contains(got, "export FOO=bar") {
			t.Errorf("lost pre-block content:\n%s", got)
		}
		if !strings.Contains(got, "alias ll='ls -la'") {
			t.Errorf("lost post-block content:\n%s", got)
		}
	})
	t.Run("no marker is a no-op", func(t *testing.T) {
		content := "just some rc\n"
		if got := RemoveStartupBlock(content); got != content {
			t.Errorf("no-op changed content: %q", got)
		}
	})
	t.Run("start without end is left untouched", func(t *testing.T) {
		content := pre + shellscript.SectionStart + "\nblobber\n"
		// Truncated/corrupt: must NOT delete the trailing blobber line.
		if got := RemoveStartupBlock(content); got != content {
			t.Errorf("truncated block should be left alone, got:\n%s", got)
		}
	})
}

func TestInjectStartupBlock_Idempotent(t *testing.T) {
	t.Parallel()
	block, _ := GuardBlock(shellscript.ShellBash, shellscript.Env{
		ProxyAddr: "127.0.0.1:8888", OverrideBinPath: "/x", CACertPath: "/x/ca",
	})
	once := InjectStartupBlock("export A=1\n", block)
	twice := InjectStartupBlock(once, block)
	if strings.Count(twice, shellscript.SectionStart) != 1 {
		t.Errorf("idempotent: expected 1 start marker, got %d\n%s",
			strings.Count(twice, shellscript.SectionStart), twice)
	}
}

func TestInjectStartupBlock_PreservesSurroundingContent(t *testing.T) {
	t.Parallel()
	block, _ := GuardBlock(shellscript.ShellBash, shellscript.Env{
		ProxyAddr: "127.0.0.1:8888", OverrideBinPath: "/x", CACertPath: "/x/ca",
	})
	content := "# header line\nalias x=1\n"
	out := InjectStartupBlock(content, block)
	if !strings.HasPrefix(out, "# header line\nalias x=1\n") {
		t.Errorf("pre-content not preserved:\n%s", out)
	}
	if !strings.Contains(out, "alias x=1\n") {
		t.Errorf("alias lost:\n%s", out)
	}
}

func TestStartupFilesFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind shellscript.ShellKind
		want []string
	}{
		{"bash", shellscript.ShellBash, []string{"/h/.bashrc", "/h/.bash_profile", "/h/.profile"}},
		{"gitbash", shellscript.ShellGitBash, []string{"/h/.bashrc", "/h/.bash_profile", "/h/.profile"}},
		{"fish", shellscript.ShellFish, []string{"/h/.config/fish/config.fish"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := startupFilesFor(tc.kind, "/h")
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestShellCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		kind    shellscript.ShellKind
		wantBin string
	}{
		{"bash", shellscript.ShellBash, "bash"},
		{"gitbash", shellscript.ShellGitBash, "bash"},
		{"fish", shellscript.ShellFish, "fish"},
		{"powershell", shellscript.ShellPowerShell, "pwsh"},
		{"unknown falls back to bash", "ksh", "bash"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bin, _ := ShellCommand(tc.kind)
			if bin != tc.wantBin {
				t.Errorf("bin = %q, want %q", bin, tc.wantBin)
			}
		})
	}
}

// --- integration test ---------------------------------------------------

// TestStartStopLifecycle wires the full orchestration with fakes: a CA is
// ensured, override-bin shims are written, a startup file is patched, the
// proxy and the local API come up, captures persist and show through the API,
// and Stop reverses everything.
func TestStartStopLifecycle(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	configDir := base // pretend this is ~/.config/wiretap for the test

	// Real on-disk SQLite so PCStore + localapi read the same rows.
	dbPath := filepath.Join(base, "wiretap.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigratePC(ctx, db); err != nil {
		t.Fatalf("MigratePC: %v", err)
	}
	pcStore := store.NewPCStore(db)

	rc := filepath.Join(base, ".bashrc")
	fakeClock := &testutil.FakeClock{T: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)}

	deps := Deps{
		ConfigDir:    configDir,
		ProxyAddr:    "127.0.0.1:0",
		LocalAPIAddr: "127.0.0.1:0",
		ShellKind:    shellscript.ShellBash,
		Installer:    &fakeInstaller{},
		PCStore:      pcStore,
		Clock:        fakeClock,
		Version:      "test",
		StartupFiles: []string{rc},
	}

	sess, err := Start(ctx, deps)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = sess.Stop(ctx) })

	if sess.ProxyAddr() == "" {
		t.Error("ProxyAddr empty after Start")
	}
	if sess.LocalAPIAddr() == "" {
		t.Error("LocalAPIAddr empty after Start")
	}

	// Override bin shims materialised.
	for _, tool := range []string{"git", "curl", "node"} {
		p := filepath.Join(configDir, "override-bin", tool)
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("override shim %s: %v", p, err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("%s perm = %o, want 0755", p, info.Mode().Perm())
		}
	}

	// Startup file patched with the gated block.
	body, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read rc: %v", err)
	}
	if !strings.Contains(string(body), shellscript.SectionStart) {
		t.Errorf("rc missing start marker:\n%s", body)
	}
	if !strings.Contains(string(body), "WIRETAP_ACTIVE") {
		t.Errorf("rc missing guard:\n%s", body)
	}

	// Local API liveness.
	health, err := http.Get("http://" + sess.LocalAPIAddr() + "/local/health")
	if err != nil {
		t.Fatalf("GET /local/health: %v", err)
	}
	if health.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.StatusCode)
	}
	_ = health.Body.Close()

	// A capture persisted to PCStore is visible through the local API.
	_, err = pcStore.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
		At:             fakeClock.Now(),
		Method:         "POST",
		URL:            "https://example.com/api",
		ReqHeadersJSON: `{"User-Agent":["test"]}`,
		ReqBody:        []byte("ping"),
		Status:         200,
		RespBody:       []byte("pong"),
	})
	if err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}

	resp, err := http.Get("http://" + sess.LocalAPIAddr() + "/local/captures?limit=10")
	if err != nil {
		t.Fatalf("GET /local/captures: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("captures status = %d, want 200", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode captures: %v\nbody=%s", err, raw)
	}
	caps, _ := out["captures"].([]any)
	if len(caps) != 1 {
		t.Fatalf("captures = %d, want 1 (body=%s)", len(caps), raw)
	}
	c0, _ := caps[0].(map[string]any)
	if c0["url"] != "https://example.com/api" {
		t.Errorf("capture url = %v", c0["url"])
	}

	// Stop reverts the startup file and removes the override dir.
	if err := sess.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// No-op Stop is safe.
	if err := sess.Stop(ctx); err != nil {
		t.Errorf("second Stop: %v", err)
	}
	cleaned, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read rc after Stop: %v", err)
	}
	if strings.Contains(string(cleaned), shellscript.SectionStart) {
		t.Errorf("rc still has start marker after Stop:\n%s", cleaned)
	}
	if _, err := os.Stat(filepath.Join(configDir, "override-bin")); !os.IsNotExist(err) {
		t.Errorf("override-bin dir should be removed after Stop (err=%v)", err)
	}
}
