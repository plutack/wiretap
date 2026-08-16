# Traffic interception

`wiretap intercept start` records outbound HTTP/HTTPS traffic from a child
shell: it starts a local MITM proxy, prints (or sources) a small shell script
that points that shell's tools at the proxy, and captures every request and
response into the same local SQLite store the dashboard reads.

This guide walks through the whole mechanism: the quickstart, what the
generated script actually does, how TLS is handled, and the per-tool
workarounds (PATH shims) that make `git`, `curl`, and `node` work without
touching your system trust store.

## Quickstart

```sh
$ wiretap intercept start
wiretap: interception proxy listening at http://127.0.0.1:8888
wiretap: control API at http://127.0.0.1:9876/local/{health,webhooks,captures}
wiretap: interception enabled for bash shell
wiretap: interception enabled
wiretap: Run wiretap_stop_interception to stop intercepting in this shell.
```

You are now in a **child shell** with interception active. Anything you run
here goes through the proxy:

```sh
$ curl https://api.example.com/v1/pets   # captured (request + response)
$ git clone https://github.com/octocat/Hello-World   # captured
$ node -e "fetch('https://api.example.com/health')"   # captured
```

Open the dashboard (`wiretap gui` in another terminal) and the **Traffic**
tab lists each exchange with full headers and bodies. Captures also surface
in the TUI and via the local control API:

```sh
curl 'http://127.0.0.1:9876/local/captures?limit=50'
```

Stop in either way:

```sh
$ exit                               # leave the child shell entirely
$ wiretap_stop_interception          # revert just this shell, keep the child open
```

Both restore your original `PATH` and environment. Exiting the child shell
also stops the proxy.

### Other shells

The `--shell` flag (or `intercept.shell` in the config) selects the flavor of
the generated script. Anything `sh`-compatible — bash, zsh, dash, ksh — uses
the same POSIX script; fish and PowerShell get native syntax:

```sh
wiretap intercept start --shell fish
wiretap intercept start --shell pwsh      # Windows PowerShell / pwsh
wiretap intercept start --shell gitbash   # Git Bash on Windows
```

Without a flag wiretap follows `$SHELL`.

### No child shell

`--no-shell` starts the proxy and API without spawning anything — useful in
scripts or CI where you want to configure the environment yourself:

```sh
wiretap intercept start --no-shell
export HTTPS_PROXY=http://127.0.0.1:8888
export SSL_CERT_FILE=~/.local/share/wiretap/ca.crt
```

Press Ctrl-C to stop.

## What the generated script does

`intercept start` generates a shell-native snippet and sources it in the
child shell. This is the bash version (the fish/PowerShell ones are
equivalent):

```sh
export WIRETAP_ACTIVE=1
__WIRETAP_OLD_PATH="$PATH"
__WIRETAP_OLD_HTTP_PROXY="${HTTP_PROXY:-}"
__WIRETAP_OLD_HTTPS_PROXY="${HTTPS_PROXY:-}"
__WIRETAP_OLD_NO_PROXY="${NO_PROXY:-}"
__WIRETAP_OLD_SSL_CERT_FILE="${SSL_CERT_FILE:-}"
__WIRETAP_OLD_NODE_EXTRA_CA_CERTS="${NODE_EXTRA_CA_CERTS:-}"
export HTTP_PROXY="http://127.0.0.1:8888"
export HTTPS_PROXY="http://127.0.0.1:8888"
export NO_PROXY="localhost,127.0.0.1"
export SSL_CERT_FILE="/home/you/.local/share/wiretap/ca.crt"
export NODE_EXTRA_CA_CERTS="/home/you/.local/share/wiretap/ca.crt"
export PATH="/home/you/.local/share/wiretap/override-bin:$PATH"

wiretap_stop_interception() {
    # ... restores every saved variable, unsets WIRETAP_ACTIVE ...
}
```

Variable by variable:

| Variable | Purpose |
|---|---|
| `WIRETAP_ACTIVE=1` | Marks the shell as intercepted; scripts and prompts can check it. |
| `HTTP_PROXY` / `HTTPS_PROXY` | Route HTTP-aware tools through the local proxy. |
| `NO_PROXY=localhost,127.0.0.1` | Keep loopback traffic direct. |
| `SSL_CERT_FILE` | Makes Go, curl, and most C tools trust the wiretap CA for HTTPS MITM. |
| `NODE_EXTRA_CA_CERTS` | Same for Node.js (which ignores `SSL_CERT_FILE`). |
| `PATH` (prepended `override-bin`) | Drops per-tool shims in front of the real binaries — see below. |

The script also pings the proxy once with `curl --noproxy '*'` so the session
can log that the shell attached successfully — this request bypasses the proxy
on purpose.

`wiretap_stop_interception` (or `Wiretap_Stop_Interception` on PowerShell)
restores everything the snippet saved, so calling it twice or editing
variables in between is safe: the restore uses the values captured at start
time, not guesses.

## TLS and the wiretap CA

To record HTTPS bodies, the proxy re-signs responses with a local
certificate authority generated on first use and stored under
`~/.local/share/wiretap/ca.crt` (`ca.pem` on Windows). The private key never
leaves your machine and the CA is unique to this install.

Tools trust that CA through the environment variables above plus, for the
three best-known holdouts, shims:

- **curl** — a shim in `override-bin` adds `--cacert <ca.crt>` to every
  invocation (curl otherwise requires `--cacert` or `CURL_CA_BUNDLE`).
- **git** — a shim appends `-c http.sslCAInfo=<ca.crt>` so HTTPS remotes
  work.
- **node** — mostly covered by `NODE_EXTRA_CA_CERTS`; the shim exists as a
  passthrough so future pinning has a place to live.

Each shim is a tiny POSIX `#!/bin/sh` script that finds the *real* binary by
walking `PATH` and skipping the wiretap shim directory itself, so shims never
recurse or shadow anything outside the intercepted shell. Delete the
`override-bin` directory (or run `wiretap intercept stop`) and your normal
tools are untouched.

### Optional: trust the CA system-wide

Some tools read only the system trust store (a system Python without
`SSL_CERT_FILE`, for example). For those, install the CA once:

```sh
sudo wiretap intercept trust-ca
```

This is the only step that needs root (Linux: writes into
`/usr/local/share/ca-certificates/` and runs `update-ca-certificates`). It is
optional — the env vars and shims cover curl, git, node, Go, and most
everything else.

## Cleaning up after a crash

If a session dies without restoring (kernel panic, `kill -9`), leftover
shims or startup-file fragments can persist. Run:

```sh
wiretap intercept stop
```

to remove the override-bin directory and revert any leftover state. The
generated shell snippet itself leaves nothing behind — it only sets
variables in the (now dead) shell.

## Scripts run during interception

Interception is where the `on_request` and `on_response` script triggers
fire — every captured exchange runs your enabled scripts before the request
leaves (or after the response arrives). See
[SCRIPTING.md](SCRIPTING.md#on_request) for the scripting walkthroughs; a
typical flow is:

1. `wiretap intercept start`
2. `wiretap gui` → Scripts tab → create an `on_request` script, e.g. tag
   every outbound JSON body with `"test_run": true`.
3. Run a request in the intercepted shell; watch the modified body in the
   Traffic tab.

`on_request` / `on_response` require interception to be running; the GUI and
TUI alone never see outbound traffic.
