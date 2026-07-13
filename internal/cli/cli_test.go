package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/config"
)

// runCmd is a small helper that executes a NewRootCmd with given args and
// returns captured stdout/stderr plus the error. With SilenceUsage &&
// SilenceErrors set on the root, this is clean output with no cobra noise.
func runCmd(t *testing.T, version string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := NewRootCmd(version)
	var out, errs bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errs.String(), err
}

// withTempConfigManager swaps the newConfigManager seam to a temp dir and
// registers a cleanup restoring the original. Returns the base dir so the
// caller can assert on resulting file paths.
func withTempConfigManager(t *testing.T) string {
	t.Helper()
	orig := newConfigManager
	base := t.TempDir()
	newConfigManager = func() *config.Manager {
		return config.NewManager(config.WithBaseDir(base))
	}
	t.Cleanup(func() { newConfigManager = orig })
	return base
}

func TestNewRootCmd_BuildsTree(t *testing.T) {
	t.Parallel()
	root := NewRootCmd("dev")
	// Four top-level subcommands: version, config, relay, tui.
	names := subcommandNames(root)
	if len(names) != 4 {
		t.Fatalf("top-level subcommands = %v, want exactly 4", names)
	}
	want := map[string]bool{"version": false, "config": false, "relay": false, "tui": false}
	for _, n := range names {
		if _, ok := want[n]; !ok {
			t.Errorf("unexpected subcommand %q", n)
		}
		want[n] = true
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("missing expected subcommand %q", n)
		}
	}
}

func TestVersionCmd_PrintsVersion(t *testing.T) {
	t.Parallel()
	out, _, err := runCmd(t, "1.2.3", "version")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "1.2.3\n" {
		t.Errorf("version output = %q, want %q", out, "1.2.3\n")
	}
}

func TestRoot_VersionFlag(t *testing.T) {
	t.Parallel()
	// `--version` is wired by setting root.Version in NewRootCmd.
	out, _, err := runCmd(t, "9.9.9", "--version")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "9.9.9") {
		t.Errorf("--version output = %q, want it to contain 9.9.9", out)
	}
}

// TestConfigInitCmd_WithTempConfigDir overrides the newConfigManager seam to
// a temp-dir-backed manager, then runs the cobra command end-to-end. This
// shows how to test commands that do filesystem I/O without touching the
// real user config dir.
func TestConfigInitCmd_WithTempConfigDir(t *testing.T) {
	base := withTempConfigManager(t)

	out, _, err := runCmd(t, "dev", "config", "init")
	if err != nil {
		t.Fatalf("config init: %v", err)
	}
	wantPath := filepath.Join(base, "wiretap", "config.yaml")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected config file at %s: %v", wantPath, err)
	}
	if !strings.Contains(out, wantPath) {
		t.Errorf("stdout = %q, want it to contain %q", out, wantPath)
	}
}

func TestConfigInitCmd_ErrorsWhenExistsAndForcesOverwrite(t *testing.T) {
	withTempConfigManager(t)

	if _, _, err := runCmd(t, "dev", "config", "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if _, _, err := runCmd(t, "dev", "config", "init"); err == nil {
		t.Fatal("second init without --force: expected error, got nil")
	}
	if _, _, err := runCmd(t, "dev", "config", "init", "--force"); err != nil {
		t.Fatalf("init --force: %v", err)
	}
}

// subcommandNames returns the Use-field name of each direct subcommand of
// root. Useful for asserting the shape of the command tree.
func subcommandNames(root *cobra.Command) []string {
	var names []string
	for _, c := range root.Commands() {
		names = append(names, c.Name())
	}
	return names
}
