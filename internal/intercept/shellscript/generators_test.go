package shellscript

import (
	"errors"
	"strings"
	"testing"

	"github.com/plutack/wiretap/internal/testutil"
)

// fullEnv is a representative Env used to produce predictable golden output.
// Every field is set so the golden files exercise all code paths.
func fullEnv() Env {
	return Env{
		ProxyAddr:       "127.0.0.1:8888",
		OverrideBinPath: "/home/user/.local/share/wiretap/override-bin",
		CACertPath:      "/home/user/.local/share/wiretap/ca.crt",
		CallbackURL:     "http://127.0.0.1:9999/callback",
	}
}

func TestGenerate_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		kind   ShellKind
		golden string
	}{
		{"bash_full", ShellBash, "bash_full.golden"},
		{"bash_no_ca", ShellBash, "bash_no_ca.golden"},
		{"bash_no_path", ShellBash, "bash_no_path.golden"},
		{"fish_full", ShellFish, "fish_full.golden"},
		{"pwsh_full", ShellPowerShell, "pwsh_full.golden"},
		{"pwsh_no_ca", ShellPowerShell, "pwsh_no_ca.golden"},
		{"gitbash_windows_path", ShellGitBash, "gitbash_windows_path.golden"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Not parallel: golden files are shared testdata; allowing -update
			// writes from parallel subtests would race. Tests run in separate
			// temp dirs mentally, but the golden files are physically on disk.
			env := fullEnv()
			switch tc.name {
			case "bash_no_ca", "pwsh_no_ca":
				env.CACertPath = ""
			case "bash_no_path":
				env.OverrideBinPath = ""
			case "gitbash_windows_path":
				env.OverrideBinPath = "C:\\Users\\dev\\wiretap\\override-bin"
			}
			got, err := Generate(tc.kind, env)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			testutil.Golden(t, tc.golden, got)
		})
	}
}

func TestGenerate_UnsupportedShell(t *testing.T) {
	t.Parallel()
	_, err := Generate(ShellKind("tcsh"), fullEnv())
	if !errors.Is(err, ErrUnsupportedShell) {
		t.Errorf("err = %v, want ErrUnsupportedShell", err)
	}
}

func TestBash_StopFunctionPresent(t *testing.T) {
	t.Parallel()
	got := Bash(fullEnv())
	if !strings.Contains(got, "wiretap_stop_interception") {
		t.Error("bash script missing wiretap_stop_interception function")
	}
	if !strings.Contains(got, "WIRETAP_ACTIVE=") {
		t.Error("bash script missing WIRETAP_ACTIVE export")
	}
	// Must restore PATH
	if !strings.Contains(got, "__WIRETAP_OLD_PATH") {
		t.Error("bash script missing PATH snapshot/restore")
	}
}

func TestFish_StopFunctionPresent(t *testing.T) {
	t.Parallel()
	got := Fish(fullEnv())
	if !strings.Contains(got, "function wiretap_stop_interception") {
		t.Error("fish script missing stop function definition")
	}
	if !strings.Contains(got, "set -e WIRETAP_ACTIVE") {
		t.Error("fish stop function must unset WIRETAP_ACTIVE")
	}
}

func TestPowerShell_StopFunctionPresent(t *testing.T) {
	t.Parallel()
	got := PowerShell(fullEnv())
	if !strings.Contains(got, "function Stop-Interception") {
		t.Error("powershell script missing Stop-Interception function")
	}
	if !strings.Contains(got, "WIRETAP_ACTIVE") {
		t.Error("powershell script missing WIRETAP_ACTIVE env var")
	}
}

func TestGitBash_PosixPath(t *testing.T) {
	t.Parallel()
	got := GitBash(Env{
		ProxyAddr:       "127.0.0.1:8888",
		OverrideBinPath: "C:\\Users\\dev\\wiretap\\override-bin",
	})
	if !strings.Contains(got, "/c/Users/dev/wiretap/override-bin") {
		t.Errorf("gitbash script should contain POSIX path, got:\n%s", got)
	}
	if strings.Contains(got, "C:\\") {
		t.Errorf("gitbash script should not contain raw Windows path")
	}
}

func TestGitBash_UnixPathUnchanged(t *testing.T) {
	t.Parallel()
	env := fullEnv() // already a unix path
	got := GitBash(env)
	if !strings.Contains(got, env.OverrideBinPath) {
		t.Error("gitbash script should preserve unix paths unchanged")
	}
}

func TestBash_NoCallbackURL_SkipsCurl(t *testing.T) {
	t.Parallel()
	env := fullEnv()
	env.CallbackURL = ""
	got := Bash(env)
	if strings.Contains(got, "curl") {
		t.Error("bash script without callback should not call curl")
	}
}

func TestSectionMarkers(t *testing.T) {
	t.Parallel()
	if SectionStart == "" || SectionEnd == "" {
		t.Error("section markers must not be empty")
	}
	if !strings.HasPrefix(SectionStart, "# --wiretap") {
		t.Error("section start must be a comment starting with --wiretap")
	}
}
