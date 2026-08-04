package shellscript

import (
	"fmt"
	"strings"
)

// bashSnapshot captures the pre-interception values of every env var we are
// about to overwrite, so wiretap_stop_interception can restore them exactly.
// It MUST be emitted before bashExports runs — otherwise the snapshot would
// capture the wiretap values themselves and "stopping" would restore the proxy
// config rather than remove it.
func bashSnapshot(env Env) string {
	var b strings.Builder
	b.WriteString("    __WIRETAP_OLD_PATH=\"$PATH\"\n")
	b.WriteString("    __WIRETAP_OLD_HTTP_PROXY=\"${HTTP_PROXY:-}\"\n")
	b.WriteString("    __WIRETAP_OLD_HTTPS_PROXY=\"${HTTPS_PROXY:-}\"\n")
	b.WriteString("    __WIRETAP_OLD_NO_PROXY=\"${NO_PROXY:-}\"\n")
	if env.CACertPath != "" {
		b.WriteString("    __WIRETAP_OLD_SSL_CERT_FILE=\"${SSL_CERT_FILE:-}\"\n")
		b.WriteString("    __WIRETAP_OLD_NODE_EXTRA_CA_CERTS=\"${NODE_EXTRA_CA_CERTS:-}\"\n")
	}
	return b.String()
}

// bashExports produces the export lines for sh-compatible shells (bash,
// zsh, dash, ksh, sh). Each env value is shell-escaped: only double quotes
// and backslashes need escaping inside double-quoted context in POSIX sh.
func bashExports(env Env) string {
	var b strings.Builder
	fmt.Fprintf(&b, "    export HTTP_PROXY=%q\n", "http://"+env.ProxyAddr)
	fmt.Fprintf(&b, "    export HTTPS_PROXY=%q\n", "http://"+env.ProxyAddr)
	// Allow the proxy itself (and localhost) to bypass the proxy.
	fmt.Fprintf(&b, "    export NO_PROXY=%q\n", "localhost,127.0.0.1")
	if env.CACertPath != "" {
		fmt.Fprintf(&b, "    export SSL_CERT_FILE=%q\n", env.CACertPath)
		fmt.Fprintf(&b, "    export NODE_EXTRA_CA_CERTS=%q\n", env.CACertPath)
	}
	if env.OverrideBinPath != "" {
		fmt.Fprintf(&b, "    export PATH=%q\n", env.OverrideBinPath+":$PATH")
	}
	return b.String()
}

// bashStopFn returns the wiretap_stop_interception function for sh-compatible
// shells. It restores every env var we set (from the snapshot bashSnapshot
// captured before interception) and unsets WIRETAP_ACTIVE so future shells
// launched from this one are clean. The snapshot+restore pattern is critical:
// simply unsetting would lose any pre-existing proxy config the user had.
func bashStopFn(env Env) string {
	restore := ""
	if env.CACertPath != "" {
		restore += `
        export SSL_CERT_FILE="$__WIRETAP_OLD_SSL_CERT_FILE"
        export NODE_EXTRA_CA_CERTS="$__WIRETAP_OLD_NODE_EXTRA_CA_CERTS"`
	}
	return `
    wiretap_stop_interception() {
        export PATH="$__WIRETAP_OLD_PATH"
        export HTTP_PROXY="$__WIRETAP_OLD_HTTP_PROXY"
        export HTTPS_PROXY="$__WIRETAP_OLD_HTTPS_PROXY"
        export NO_PROXY="$__WIRETAP_OLD_NO_PROXY"` + restore + `
        unset __WIRETAP_OLD_PATH __WIRETAP_OLD_HTTP_PROXY __WIRETAP_OLD_HTTPS_PROXY __WIRETAP_OLD_NO_PROXY` + stopOldExtrasUnset(env) + `
        unset WIRETAP_ACTIVE
        echo 'wiretap: interception disabled in this shell'
    }`
}

func stopOldExtrasUnset(env Env) string {
	if env.CACertPath != "" {
		return " __WIRETAP_OLD_SSL_CERT_FILE __WIRETAP_OLD_NODE_EXTRA_CA_CERTS"
	}
	return ""
}

// Bash returns the sourceable script for bash/zsh/dash/ksh/sh. It exports
// the proxy env, defines wiretap_stop_interception, and (optionally) pings
// the callback URL to tell the app interception is live.
func Bash(env Env) string {
	var b strings.Builder
	b.WriteString("    export WIRETAP_ACTIVE=1\n")
	// Snapshot the user's original env BEFORE we overwrite it, so the stop
	// function restores their real values rather than wiretap's.
	b.WriteString(bashSnapshot(env))
	b.WriteString(bashExports(env))
	b.WriteString(bashStopFn(env))
	if env.CallbackURL != "" {
		fmt.Fprintf(&b, "\n    if command -v curl >/dev/null 2>&1; then\n        (curl --noproxy '*' -X POST %q >/dev/null 2>&1 &) &> /dev/null\n    fi", env.CallbackURL)
	}
	b.WriteString("\n    echo 'wiretap: interception enabled'\n    echo 'Run wiretap_stop_interception to stop intercepting in this shell.'\n")
	return b.String()
}

// GitBash is Bash with the OverrideBinPath translated from Windows (C:\...)
// to POSIX form (/c/...) since Git Bash expects Unix-style PATH entries.
func GitBash(env Env) string {
	env.OverrideBinPath = toPosixPath(env.OverrideBinPath)
	return Bash(env)
}

// toPosixPath converts a Windows-style path (C:\Users\...) to a POSIX-style
// path (/c/Users/...) as Git Bash expects. If the path doesn't look like a
// Windows path it is returned unchanged.
func toPosixPath(p string) string {
	if len(p) < 2 || p[1] != ':' {
		return p
	}
	drive := strings.ToLower(string(p[0]))
	rest := strings.ReplaceAll(p[2:], "\\", "/")
	return "/" + drive + rest
}

// fishRestoreOrErase emits a fish snippet that restores an exported variable
// from its __WIRETAP_OLD_<name> snapshot, or erases it when the snapshot is
// empty (the user had no such variable before interception). `set -q var[1]`
// is false for a global set to zero elements, which is what the snapshot
// captures when the original was unset. Uses `set -gx` so the change reaches
// the interactive shell rather than staying local to the stop function.
func fishRestoreOrErase(name string) string {
	return fmt.Sprintf("        if set -q __WIRETAP_OLD_%[1]s[1]\n"+
		"            set -gx %[1]s $__WIRETAP_OLD_%[1]s\n"+
		"        else\n"+
		"            set -e %[1]s\n"+
		"        end\n", name)
}

// Fish returns the script for the fish shell. Fish uses set -x for exports
// and function/endfunction for function definitions.
func Fish(env Env) string {
	var b strings.Builder
	b.WriteString("    set -x WIRETAP_ACTIVE 1\n")
	// Snapshot the user's original env BEFORE we overwrite it, so the stop
	// function restores their real values rather than wiretap's.
	b.WriteString("    set -g __WIRETAP_OLD_PATH $PATH\n")
	b.WriteString("    set -g __WIRETAP_OLD_HTTP_PROXY $HTTP_PROXY\n")
	b.WriteString("    set -g __WIRETAP_OLD_HTTPS_PROXY $HTTPS_PROXY\n")
	b.WriteString("    set -g __WIRETAP_OLD_NO_PROXY $NO_PROXY\n")
	if env.CACertPath != "" {
		b.WriteString("    set -g __WIRETAP_OLD_SSL_CERT_FILE $SSL_CERT_FILE\n")
		b.WriteString("    set -g __WIRETAP_OLD_NODE_EXTRA_CA_CERTS $NODE_EXTRA_CA_CERTS\n")
	}
	fmt.Fprintf(&b, "    set -x HTTP_PROXY %q\n", "http://"+env.ProxyAddr)
	fmt.Fprintf(&b, "    set -x HTTPS_PROXY %q\n", "http://"+env.ProxyAddr)
	fmt.Fprintf(&b, "    set -x NO_PROXY %q\n", "localhost,127.0.0.1")
	if env.CACertPath != "" {
		fmt.Fprintf(&b, "    set -x SSL_CERT_FILE %q\n", env.CACertPath)
		fmt.Fprintf(&b, "    set -x NODE_EXTRA_CA_CERTS %q\n", env.CACertPath)
	}
	if env.OverrideBinPath != "" {
		fmt.Fprintf(&b, "    set -x PATH %q $PATH\n", env.OverrideBinPath)
	}
	// Define stop function. Restores run with `set -gx` on purpose: a bare
	// `set -x` inside a fish function is scoped to the function body and would
	// vanish on return, so the shell would keep wiretap's proxy config. Each
	// proxy var is restored only if the user actually had one; otherwise it is
	// erased so no empty variable lingers. PATH is always restored (never
	// erased). The function erases itself last so `stop` leaves no trace.
	b.WriteString("    function wiretap_stop_interception\n")
	b.WriteString("        set -gx PATH $__WIRETAP_OLD_PATH\n")
	b.WriteString(fishRestoreOrErase("HTTP_PROXY"))
	b.WriteString(fishRestoreOrErase("HTTPS_PROXY"))
	b.WriteString(fishRestoreOrErase("NO_PROXY"))
	if env.CACertPath != "" {
		b.WriteString(fishRestoreOrErase("SSL_CERT_FILE"))
		b.WriteString(fishRestoreOrErase("NODE_EXTRA_CA_CERTS"))
		b.WriteString("        set -e __WIRETAP_OLD_SSL_CERT_FILE\n")
		b.WriteString("        set -e __WIRETAP_OLD_NODE_EXTRA_CA_CERTS\n")
	}
	b.WriteString("        set -e __WIRETAP_OLD_PATH __WIRETAP_OLD_HTTP_PROXY __WIRETAP_OLD_HTTPS_PROXY __WIRETAP_OLD_NO_PROXY\n")
	b.WriteString("        set -e WIRETAP_ACTIVE\n")
	b.WriteString("        echo 'wiretap: interception disabled in this shell'\n")
	b.WriteString("        functions -e wiretap_stop_interception\n")
	b.WriteString("    end\n")
	if env.CallbackURL != "" {
		fmt.Fprintf(&b, "    if command -v curl >/dev/null 2>&1\n        curl --noproxy '*' -X POST %q >/dev/null 2>&1 &\n    end\n", env.CallbackURL)
	}
	b.WriteString("    echo 'wiretap: interception enabled'\n")
	b.WriteString("    echo 'Run wiretap_stop_interception to stop intercepting in this shell.'\n")
	return b.String()
}

// PowerShell returns the script for Windows PowerShell. It sets env vars,
// overrides Invoke-WebRequest defaults, and defines the Stop-Interception
// function with wiretap's naming.
func PowerShell(env Env) string {
	var b strings.Builder
	// Snapshot the current env BEFORE we set anything (including WIRETAP_ACTIVE)
	// so Stop-Interception restores the user's real values and does not leave
	// WIRETAP_ACTIVE behind.
	b.WriteString("    $__WIRETAP_OLD_ENV = Get-ChildItem Env:\n")
	b.WriteString("    $Env:WIRETAP_ACTIVE = \"1\"\n")
	fmt.Fprintf(&b, "    $Env:HTTP_PROXY = %q\n", "http://"+env.ProxyAddr)
	fmt.Fprintf(&b, "    $Env:HTTPS_PROXY = %q\n", "http://"+env.ProxyAddr)
	fmt.Fprintf(&b, "    $Env:NO_PROXY = %q\n", "localhost,127.0.0.1")
	if env.CACertPath != "" {
		fmt.Fprintf(&b, "    $Env:SSL_CERT_FILE = %q\n", env.CACertPath)
		fmt.Fprintf(&b, "    $Env:NODE_EXTRA_CA_CERTS = %q\n", env.CACertPath)
	}
	if env.OverrideBinPath != "" {
		fmt.Fprintf(&b, "    $Env:PATH = %q + \";\" + $Env:PATH\n", env.OverrideBinPath)
	}
	// Override Invoke-WebRequest defaults to use the proxy and skip cert
	// checks (the proxy handles HTTPS upstream).
	b.WriteString("    $PSDefaultParameterValues[\"invoke-webrequest:proxy\"] = $Env:HTTP_PROXY\n")
	b.WriteString("    $PSDefaultParameterValues[\"invoke-webrequest:SkipCertificateCheck\"] = $True\n")
	// Define Stop-Interception function
	b.WriteString("\n    function Stop-Interception {\n")
	b.WriteString("        foreach ($var in (Get-ChildItem Env:)) {\n")
	b.WriteString("            [System.Environment]::SetEnvironmentVariable($var.Name, $null)\n")
	b.WriteString("        }\n")
	b.WriteString("        foreach ($var in $__WIRETAP_OLD_ENV) {\n")
	b.WriteString("            [System.Environment]::SetEnvironmentVariable($var.Name, $var.Value)\n")
	b.WriteString("        }\n")
	b.WriteString("        $PSDefaultParameterValues.Remove(\"invoke-webrequest:proxy\")\n")
	b.WriteString("        $PSDefaultParameterValues.Remove(\"invoke-webrequest:SkipCertificateCheck\")\n")
	b.WriteString("        Write-Host 'wiretap: interception disabled in this shell'\n")
	b.WriteString("    }\n")
	if env.CallbackURL != "" {
		fmt.Fprintf(&b, "    Start-Job -ScriptBlock { Invoke-WebRequest %q -NoProxy -Method 'POST' } | Out-Null\n", env.CallbackURL)
	}
	b.WriteString("    Write-Host \"wiretap: interception enabled`nTo stop intercepting type \" -NoNewline\n")
	b.WriteString("    Write-Host \"Stop-Interception\" -ForegroundColor Red\n")
	return b.String()
}

// GenerateErr is a convenience wrapper for callers that want the error
// without importing errors themselves. Currently just exposes
// ErrUnsupportedShell; kept for API symmetry with future versions.
func GenerateErr(kind ShellKind, env Env) (string, error) {
	return Generate(kind, env)
}
