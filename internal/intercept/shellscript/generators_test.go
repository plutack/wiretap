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
		// Both listeners, as actually bound: the control API deliberately uses a
		// different port from the proxy so the golden proves NO_PROXY is derived
		// from the addresses rather than hard-coded.
		SelfAddrs: []string{"127.0.0.1:8888", "127.0.0.1:9876"},
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
		{"fish_no_ca", ShellFish, "fish_no_ca.golden"},
		{"fish_no_path", ShellFish, "fish_no_path.golden"},
		{"pwsh_full", ShellPowerShell, "pwsh_full.golden"},
		{"pwsh_no_ca", ShellPowerShell, "pwsh_no_ca.golden"},
		{"pwsh_no_path", ShellPowerShell, "pwsh_no_path.golden"},
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
			case "bash_no_ca", "fish_no_ca", "pwsh_no_ca":
				env.CACertPath = ""
			case "bash_no_path", "fish_no_path", "pwsh_no_path":
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

// TestGenerators_ExportBothProxyVarCases pins the fix for plain-HTTP traffic
// bypassing the proxy: curl refuses the uppercase HTTP_PROXY for http:// URLs
// (a deliberate injection guard), so a shell that exports only the uppercase
// spelling silently sends plain HTTP straight to the internet.
func TestGenerators_ExportBothProxyVarCases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		got  string
	}{
		{"bash", Bash(fullEnv())},
		{"fish", Fish(fullEnv())},
		{"powershell", PowerShell(fullEnv())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
				if !strings.Contains(tc.got, want) {
					t.Errorf("generated script does not set %s", want)
				}
			}
		})
	}
}

// TestBash_RestoresBothProxyVarCases guards the snapshot/restore symmetry: a
// lowercase variable we set must be restored (or erased) by the stop function,
// otherwise stopping interception would leave the shell pointed at a dead proxy.
func TestBash_RestoresBothProxyVarCases(t *testing.T) {
	t.Parallel()
	got := Bash(fullEnv())
	for _, want := range []string{
		`__WIRETAP_OLD_http_proxy="${http_proxy:-}"`,
		`__WIRETAP_OLD_https_proxy="${https_proxy:-}"`,
		`__WIRETAP_OLD_no_proxy="${no_proxy:-}"`,
		`export http_proxy="$__WIRETAP_OLD_http_proxy"`,
		`export no_proxy="$__WIRETAP_OLD_no_proxy"`,
		"__WIRETAP_OLD_http_proxy __WIRETAP_OLD_https_proxy __WIRETAP_OLD_no_proxy",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
}

func TestFish_RestoresBothProxyVarCases(t *testing.T) {
	t.Parallel()
	got := Fish(fullEnv())
	for _, want := range []string{
		"set -g __WIRETAP_OLD_http_proxy $http_proxy",
		"set -gx http_proxy $__WIRETAP_OLD_http_proxy",
		"set -e __WIRETAP_OLD_http_proxy __WIRETAP_OLD_https_proxy __WIRETAP_OLD_no_proxy",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("fish script missing %q", want)
		}
	}
}

// TestGenerate_NoProxyComesFromBoundAddresses is the "no hard-coded ports" test:
// NO_PROXY must carry the addresses this session actually bound, including the
// control API on its own port, so loopback services other than wiretap's stay
// interceptable.
func TestGenerate_NoProxyComesFromBoundAddresses(t *testing.T) {
	t.Parallel()
	env := fullEnv()
	env.ProxyAddr = "127.0.0.1:54321"
	env.SelfAddrs = []string{"127.0.0.1:54321", "127.0.0.1:54322"}

	got := Bash(env)
	if !strings.Contains(got, `export NO_PROXY="127.0.0.1:54321,localhost:54321,127.0.0.1:54322,localhost:54322"`) {
		t.Errorf("NO_PROXY not derived from the bound addresses; got:\n%s", got)
	}
	if strings.Contains(got, "9876") || strings.Contains(got, "8888") {
		t.Error("generated script contains a hard-coded wiretap port")
	}
	// A different port must not be excluded, or the whole point is lost.
	if strings.Contains(got, "1700") {
		t.Error("unrelated loopback service ended up in NO_PROXY")
	}
}

func TestNoProxyList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"loopback ip gains localhost spelling", []string{"127.0.0.1:8888"}, "127.0.0.1:8888,localhost:8888"},
		{"localhost gains ip spelling", []string{"localhost:8888"}, "localhost:8888,127.0.0.1:8888"},
		{"ipv6 loopback", []string{"[::1]:8888"}, "[::1]:8888,localhost:8888"},
		{"deduplicates", []string{"127.0.0.1:8888", "127.0.0.1:8888"}, "127.0.0.1:8888,localhost:8888"},
		{"non-loopback is passed through untouched", []string{"10.0.0.5:8888"}, "10.0.0.5:8888"},
		{"portless entry", []string{"example.test"}, "example.test"},
		{"empty", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NoProxyList(tc.in); got != tc.want {
				t.Errorf("NoProxyList(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNoProxyValue_AlwaysCoversProxyAddr: a caller that forgets SelfAddrs must
// still not let the proxy be asked to dial itself.
func TestNoProxyValue_AlwaysCoversProxyAddr(t *testing.T) {
	t.Parallel()
	got := noProxyValue(Env{ProxyAddr: "127.0.0.1:4444"})
	if got != "127.0.0.1:4444,localhost:4444" {
		t.Errorf("noProxyValue = %q, want the proxy address included", got)
	}
}
