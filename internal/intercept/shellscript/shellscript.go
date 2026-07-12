// Package shellscript generates the sourceable shell scripts that wiretap
// injects into a spawned terminal to route its HTTP/HTTPS traffic through
// the local MITM proxy. This is the Go port of httptoolkit's
// terminal-scripts.ts, adapted to wiretap's naming (WIRETAP_ACTIVE,
// WIRETAP_OVERRIDE_BIN) and with a wiretap_stop_interception function
// injected into every shell — not just PowerShell as in the original.
//
// The package is pure: every generator is a function (Env) -> string. There
// is no I/O, no filesystem, no clock. This makes the output trivially
// golden-testable: nail the expected output once, lock it down, and any
// accidental change shows up immediately.
//
// The interception lifecycle (writing startup files, spawning shells) lives
// in the parent intercept package; this one only formats text.
package shellscript

// Env carries the values a generated script exports into the shell. Fields
// map 1:1 to environment variables used by the proxy and its override-bin
// shims (git/curl/node wrappers that respect the proxy).
type Env struct {
	// ProxyAddr is the host:port of the local MITM proxy, e.g. "127.0.0.1:8888".
	ProxyAddr string
	// OverrideBinPath is the absolute path to the directory containing shim
	// scripts. Prepended to PATH so the shell finds our wrappers first.
	OverrideBinPath string
	// CACertPath is the absolute path to the CA certificate the proxy uses
	// for TLS interception. Exported as SSL_CERT_FILE / NODE_EXTRA_CA_CERTS
	// so language runtimes trust our synthetic cert chain.
	CACertPath string
	// CallbackURL is an optional HTTP endpoint the script POSTs to on
	// successful interception (lets the app know the shell started). Empty
	// disables the callback.
	CallbackURL string
}

// ShellKind enumerates the shell families we support. Each is wired to a
// generator function in generators.go.
type ShellKind string

const (
	ShellBash       ShellKind = "bash" // bash, zsh, dash, ksh, sh (sh-compatible)
	ShellFish       ShellKind = "fish"
	ShellPowerShell ShellKind = "powershell"
	ShellGitBash    ShellKind = "gitbash" // bash on Windows with POSIX path translation
)

// Generate returns the script for the given shell. Unknown kinds return
// ErrUnsupportedShell so callers can surface a clear error.
func Generate(kind ShellKind, env Env) (string, error) {
	switch kind {
	case ShellBash:
		return Bash(env), nil
	case ShellFish:
		return Fish(env), nil
	case ShellPowerShell:
		return PowerShell(env), nil
	case ShellGitBash:
		return GitBash(env), nil
	default:
		return "", ErrUnsupportedShell
	}
}

// SectionStart and SectionEnd delimit the gated config block appended to
// shell startup files (.profile, .bashrc, .zshrc, .config/fish/config.fish).
// The block only activates when WIRETAP_ACTIVE is set, so normal shells are
// untouched. resetShellStartupScripts removes everything between these
// markers before re-writing (idempotent).
const (
	SectionStart = "# --wiretap-intercept--"
	SectionEnd   = "# --wiretap-intercept-end--"
)
