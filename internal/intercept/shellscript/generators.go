package shellscript

import (
	"fmt"
	"strings"
)

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
// shells. It restores every env var we set (by snapshotting the old values
// before interception) and unsets WIRETAP_ACTIVE so future shells launched
// from this one are clean. The snapshot+restore pattern is critical: simply
// unsetting would lose any pre-existing proxy config the user had.
func bashStopFn(env Env) string {
	snapshot := ""
	restore := ""
	if env.CACertPath != "" {
		snapshot += `
    __WIRETAP_OLD_SSL_CERT_FILE="${SSL_CERT_FILE:-}"
    __WIRETAP_OLD_NODE_EXTRA_CA_CERTS="${NODE_EXTRA_CA_CERTS:-}"`
		restore += `
        export SSL_CERT_FILE="$__WIRETAP_OLD_SSL_CERT_FILE"
        export NODE_EXTRA_CA_CERTS="$__WIRETAP_OLD_NODE_EXTRA_CA_CERTS"`
	}
	return `
    __WIRETAP_OLD_PATH="$PATH"
    __WIRETAP_OLD_HTTP_PROXY="${HTTP_PROXY:-}"
    __WIRETAP_OLD_HTTPS_PROXY="${HTTPS_PROXY:-}"
    __WIRETAP_OLD_NO_PROXY="${NO_PROXY:-}"` + snapshot + `

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

// Fish returns the script for the fish shell. Fish uses set -x for exports
// and function/endfunction for function definitions.
func Fish(env Env) string {
	var b strings.Builder
	b.WriteString("    set -x WIRETAP_ACTIVE 1\n")
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
	// Snapshot old values
	b.WriteString("    set -g __WIRETAP_OLD_PATH $PATH\n")
	b.WriteString("    set -g __WIRETAP_OLD_HTTP_PROXY $HTTP_PROXY\n")
	b.WriteString("    set -g __WIRETAP_OLD_HTTPS_PROXY $HTTPS_PROXY\n")
	b.WriteString("    set -g __WIRETAP_OLD_NO_PROXY $NO_PROXY\n")
	if env.CACertPath != "" {
		b.WriteString("    set -g __WIRETAP_OLD_SSL_CERT_FILE $SSL_CERT_FILE\n")
		b.WriteString("    set -g __WIRETAP_OLD_NODE_EXTRA_CA_CERTS $NODE_EXTRA_CA_CERTS\n")
	}
	// Define stop function
	b.WriteString("    function wiretap_stop_interception\n")
	b.WriteString("        set -x PATH $__WIRETAP_OLD_PATH\n")
	b.WriteString("        set -x HTTP_PROXY $__WIRETAP_OLD_HTTP_PROXY\n")
	b.WriteString("        set -x HTTPS_PROXY $__WIRETAP_OLD_HTTPS_PROXY\n")
	b.WriteString("        set -x NO_PROXY $__WIRETAP_OLD_NO_PROXY\n")
	if env.CACertPath != "" {
		b.WriteString("        set -x SSL_CERT_FILE $__WIRETAP_OLD_SSL_CERT_FILE\n")
		b.WriteString("        set -x NODE_EXTRA_CA_CERTS $__WIRETAP_OLD_NODE_EXTRA_CA_CERTS\n")
		b.WriteString("        set -e __WIRETAP_OLD_SSL_CERT_FILE\n")
		b.WriteString("        set -e __WIRETAP_OLD_NODE_EXTRA_CA_CERTS\n")
	}
	b.WriteString("        set -e __WIRETAP_OLD_PATH __WIRETAP_OLD_HTTP_PROXY __WIRETAP_OLD_HTTPS_PROXY __WIRETAP_OLD_NO_PROXY\n")
	b.WriteString("        set -e WIRETAP_ACTIVE\n")
	b.WriteString("        echo 'wiretap: interception disabled in this shell'\n")
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
// function (adapted from httptoolkit with our naming).
func PowerShell(env Env) string {
	var b strings.Builder
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
	// Snapshot the current env so Stop-Interception can restore it.
	b.WriteString("    $__WIRETAP_OLD_ENV = Get-ChildItem Env:\n")
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
