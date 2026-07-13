package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutack/wiretap/internal/intercept"
	"github.com/plutack/wiretap/internal/intercept/shellscript"
)

// TestInterceptStopCmd_CleansLeftoverBlocks verifies `wiretap intercept stop`
// reverses startup-file blocks wiretap left behind (e.g. after a crash). We
// stub $HOME to a temp dir, seed a .bashrc with a guarded block, then ask the
// command to reset it. detectShellKind's default is ShellBash so no flags are
// needed.
func TestInterceptStopCmd_CleansLeftoverBlocks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := withTempConfigManager(t)

	// Seed ~/.bashrc: user content + a wiretap guarded block + more content.
	block, err := intercept.GuardBlock(shellscript.ShellBash, shellscript.Env{
		ProxyAddr: "127.0.0.1:8888", OverrideBinPath: "/x", CACertPath: "/x/ca",
	})
	if err != nil {
		t.Fatalf("GuardBlock: %v", err)
	}
	rcPath := filepath.Join(home, ".bashrc")
	original := "# my bashrc\nalias ll='ls -la'\n" + block + "\necho hi\n"
	if err := os.WriteFile(rcPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seed rc: %v", err)
	}

	out, _, err := runCmd(t, "dev", "intercept", "stop", "--shell", "bash")
	if err != nil {
		t.Fatalf("intercept stop: %v", err)
	}
	if !strings.Contains(out, "reset interception") {
		t.Errorf("stdout = %q, want it to mention reset", out)
	}

	got, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("read rc: %v", err)
	}
	if strings.Contains(string(got), shellscript.SectionStart) {
		t.Errorf("rc still contains start marker after stop:\n%s", got)
	}
	if !strings.Contains(string(got), "alias ll='ls -la'") {
		t.Errorf("stop lost surrounding rc content:\n%s", got)
	}
	if !strings.Contains(string(got), "echo hi") {
		t.Errorf("stop lost trailing rc content:\n%s", got)
	}

	// --shell bash → the summary line reports bash, regardless of $SHELL.
	if !strings.Contains(out, "for bash") {
		t.Errorf("stdout should name the bash shell: %q", out)
	}
	_ = base // config dir was temp-backed; nothing else to assert on it
}

// TestInterceptStartCmd_WiresDepsFromConfig swaps the interceptStart seam to a
// stub that captures the deps, so we can assert `wiretap intercept start`
// builds them from config (addrs, shell, version) without binding real ports
// or spawning a real shell. SHELL is pinned so detection is deterministic.
func TestInterceptStartCmd_WiresDepsFromConfig(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	base := withTempConfigManager(t)

	// Write a config so Load() succeeds and we can read intercept addrs back.
	if _, _, err := runCmd(t, "dev", "config", "init"); err != nil {
		t.Fatalf("config init: %v", err)
	}

	var captured intercept.Deps
	origStart := interceptStart
	origSpawn := interceptSpawn
	interceptStart = func(ctx context.Context, deps intercept.Deps) (*intercept.Session, error) {
		captured = deps
		// Return a bare Session: ProxyAddr/LocalAPIAddr read zero values; the
		// command only prints them and then calls the (stubbed) spawn.
		return &intercept.Session{}, nil
	}
	interceptSpawn = func(*intercept.Session, context.Context, shellscript.ShellKind) error {
		return nil // do not exec a real shell
	}
	t.Cleanup(func() {
		interceptStart = origStart
		interceptSpawn = origSpawn
	})

	out, _, err := runCmd(t, "testver", "intercept", "start")
	if err != nil {
		t.Fatalf("intercept start: %v", err)
	}
	_ = base

	if captured.ProxyAddr != "127.0.0.1:8888" {
		t.Errorf("ProxyAddr = %q, want default 127.0.0.1:8888", captured.ProxyAddr)
	}
	if captured.LocalAPIAddr != "127.0.0.1:9876" {
		t.Errorf("LocalAPIAddr = %q, want default 127.0.0.1:9876", captured.LocalAPIAddr)
	}
	if captured.Version != "testver" {
		t.Errorf("Version = %q, want testver", captured.Version)
	}
	if captured.ShellKind != shellscript.ShellBash {
		t.Errorf("ShellKind = %v, want ShellBash (default)", captured.ShellKind)
	}
	if captured.Installer == nil {
		t.Error("Installer should be wired (castore.NewInstaller)")
	}
	if captured.PCStore == nil {
		t.Error("PCStore should be wired")
	}
	// The command prints the resolved (zero-value) addrs from the stub session.
	if !strings.Contains(out, "interception enabled for bash shell") {
		t.Errorf("stdout missing confirmation: %q", out)
	}
}

// TestDetectShellKind exercises the priority chain flag → config → $SHELL.
// Each case sets $SHELL explicitly (t.Setenv is scoped to the test), then
// asserts what detectShellKind resolves.
func TestDetectShellKind(t *testing.T) {
	tests := []struct {
		name  string
		flag  string
		cfg   string
		shell string
		want  shellscript.ShellKind
	}{
		{"flag wins over config and SHELL", "bash", "fish", "/bin/fish", shellscript.ShellBash},
		{"config used when flag empty", "", "fish", "/bin/fish", shellscript.ShellFish},
		{"SHELL used when flag+config empty", "", "", "/bin/fish", shellscript.ShellFish},
		{"sh-compatible falls to bash", "zsh", "", "", shellscript.ShellBash},
		{"powershell kind", "powershell", "", "", shellscript.ShellPowerShell},
		{"gitbash kind", "gitbash", "", "", shellscript.ShellGitBash},
		{"default fallback from SHELL=bash", "", "", "/bin/bash", shellscript.ShellBash},
		{"unknown SHELL falls to bash", "", "", "/usr/bin/noshell", shellscript.ShellBash},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHELL", tc.shell)
			got := detectShellKind(tc.flag, tc.cfg)
			if got != tc.want {
				t.Errorf("detectShellKind(flag=%q, cfg=%q, SHELL=%q) = %v, want %v",
					tc.flag, tc.cfg, tc.shell, got, tc.want)
			}
		})
	}
}
