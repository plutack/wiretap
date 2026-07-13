// Package overridebin generates the executable shim scripts wiretap drops into
// a directory prepended to PATH so that common command-line tools route their
// HTTP(S) traffic through the local interception proxy and trust the wiretap CA.
//
// The package is pure text generation (Shim → string) plus a thin Write helper
// that persists the shims with mode 0755. The shims are POSIX sh (a #!/bin/sh
// shebang), so they work regardless of the user's interactive shell — the OS
// resolves the interpreter, not the caller. The intercept orchestrator decides
// where to drop them via env.OverrideBinPath.
//
// Each shim resolves the *real* tool by walking PATH and skipping the wiretap
// override directory (so it never re-execs itself), then execs the real binary
// with the tool-specific flags that pin the proxy and CA. Tools that already
// honour the HTTP(S)_PROXY / NODE_EXTRA_CA_CERTS env exported by the shell
// startup script (e.g. node, and most go programs) ship a passthrough shim so
// overriding them on PATH is harmless.
package overridebin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsupportedTool is returned by Shim for a Tool the package can't generate.
var ErrUnsupportedTool = errors.New("overridebin: unsupported tool")

// Env describes the interception context the shims run under. Values are
// shell-escaped before embedding, so callers may pass paths/addresses
// containing spaces.
type Env struct {
	// ProxyAddr is host:port of the local interception proxy (e.g. "127.0.0.1:8888").
	ProxyAddr string
	// CACertPath is the CA cert file passed to tools that can pin a CA. May be
	// empty for tools that read it from the environment instead.
	CACertPath string
	// OverrideBinPath is the absolute path of the directory holding the shims.
	// Each shim skips this entry while resolving the real binary so it does
	// not recurse into itself.
	OverrideBinPath string
}

// Tool names a tool we ship a shim for.
type Tool string

const (
	ToolGit  Tool = "git"
	ToolCurl Tool = "curl"
	ToolNode Tool = "node"
)

// Tools returns the set of shim generators wiretap ships, in a stable order
// (used by Write and by the orchestrator to know what to expect on disk).
func Tools() []Tool { return []Tool{ToolGit, ToolCurl, ToolNode} }

// Shim returns the POSIX sh script for one tool. The env values are escaped
// so the output is safe to exec.
func Shim(tool Tool, env Env) (string, error) {
	switch tool {
	case ToolGit, ToolCurl, ToolNode:
	default:
		return "", ErrUnsupportedTool
	}

	var b strings.Builder
	fmt.Fprintf(&b, "#!/bin/sh\n# wiretap %s shim — generated; do not edit by hand.\n", tool)
	b.WriteString(resolveHeader(env, string(tool)))

	switch tool {
	case ToolGit:
		fmt.Fprintf(&b, "exec \"$__WT_REAL\" \\\n")
		fmt.Fprintf(&b, "    -c %s \\\n", shellQuote("http.proxy=http://"+env.ProxyAddr))
		fmt.Fprintf(&b, "    -c %s \\\n", shellQuote("https.proxy=http://"+env.ProxyAddr))
		fmt.Fprintf(&b, "    -c %s \\\n", shellQuote("http.sslCAInfo="+env.CACertPath))
		fmt.Fprintf(&b, "    -- \\\n")
		fmt.Fprintf(&b, "    \"$@\"\n")
	case ToolCurl:
		fmt.Fprintf(&b, "exec \"$__WT_REAL\" \\\n")
		fmt.Fprintf(&b, "    --proxy %s \\\n", shellQuote("http://"+env.ProxyAddr))
		fmt.Fprintf(&b, "    --noproxy %s \\\n", shellQuote("localhost,127.0.0.1"))
		fmt.Fprintf(&b, "    --cacert %s \\\n", shellQuote(env.CACertPath))
		fmt.Fprintf(&b, "    \"$@\"\n")
	case ToolNode:
		// node already honours NODE_EXTRA_CA_CERTS (exported by the shell
		// startup script) for CA trust; libraries that read HTTP_PROXY already
		// pick up the proxy env. The shim exists only so the override-dir PATH
		// entry doesn't shadow node when both are present.
		fmt.Fprintf(&b, "exec \"$__WT_REAL\" \"$@\"\n")
	}
	return b.String(), nil
}

// resolveHeader returns the shared resolver that finds the real binary by
// skipping OverrideBinPath on PATH, then exec'ing it. toolName is the basename
// the resolver looks for.
func resolveHeader(env Env, toolName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "__WT_OVERRIDE_DIR=%s\n", shellQuote(env.OverrideBinPath))
	b.WriteString(strings.TrimSpace(`
__WT_resolve() {
    ifs_save="$IFS"; IFS=:
    for d in $PATH; do
        [ "$d" = "$__WT_OVERRIDE_DIR" ] && continue
        if [ -x "$d/` + toolName + `" ]; then
            printf '%s' "$d/` + toolName + `"; IFS="$ifs_save"; return 0
        fi
    done
    IFS="$ifs_save"
    return 1
}
__WT_REAL=$(__WT_resolve) || __WT_REAL=` + shellQuote(toolName) + `
`))
	b.WriteString("\n")
	return b.String()
}

// Write persists a shim for every supported tool under dir with mode 0755 and
// creates dir if needed. The orchestrator calls this once per interception
// session into a fresh per-session directory.
func Write(dir string, env Env) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("overridebin: mkdir %s: %w", dir, err)
	}
	for _, tool := range Tools() {
		body, err := Shim(tool, env)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, string(tool))
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			return fmt.Errorf("overridebin: write %s: %w", path, err)
		}
	}
	return nil
}

// shellQuote returns s wrapped in single quotes, with any embedded single
// quote escaped as the sh '\” sequence. This produces a literal that is safe
// to splice into a POSIX sh script.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
