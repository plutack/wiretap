---
title: CLI reference
description: Wiretap command groups and common flags.
---

Run `wiretap <command> --help` for the flags shipped by your installed version.

## Core commands

| Command | Purpose |
| --- | --- |
| `wiretap config init` | Create the default configuration file |
| `wiretap config import <client-file>` | Install a portable relay client identity |
| `wiretap gui` | Open the desktop dashboard; requires a GUI-enabled build |
| `wiretap tui` | Open the terminal dashboard |
| `wiretap version` | Print the embedded version |
| `wiretap completion <shell>` | Generate shell completion |

`wiretap config init --force` replaces an existing config file.
`wiretap config import --force <client-file>` is required when the file would
replace a different local relay identity. The imported bearer token prefers the
operating-system keyring; delete the handoff file after use.

## Client diagnostic logs

The desktop, TUI, and operational CLI commands write structured diagnostics to a
file, **never to stdout/stderr**. This keeps Bubble Tea rendering, intercepted
shells, and machine-readable command output intact. Help/version output does not
open a diagnostic file.

| Flag | Environment | Default |
| --- | --- | --- |
| `--log-file` | `WIRETAP_LOG_FILE` | `<config-dir>/logs/<command>.log` |
| `--log-format` | `WIRETAP_LOG_FORMAT` | `text` (`json` is also supported) |
| `--log-level` | `WIRETAP_LOG_LEVEL` | `info` (`debug`, `warn`, `error` also supported) |

Flags override environment variables. Default filenames follow the command path:
`wiretap-tui.log`, `wiretap-gui.log`, and `wiretap-intercept-start.log`, for example.
The config directory is Wiretap's usual OS-specific directory (typically
`~/.config/wiretap` on Linux). To use a known destination:

```sh
wiretap tui --log-file /tmp/wiretap-tui.log --log-format json --log-level debug
# Read this from another terminal, not inside the TUI's terminal:
tail -f /tmp/wiretap-tui.log
```

Files are append-only until approximately 10 MiB, then reset in place; this is a
bounded diagnostic history, not an audit archive. Files use owner-only permissions
on Unix. Use different files for simultaneous processes, especially with an
explicit `--log-file` override. Log destinations must be regular files, not `-`,
devices, or symlinks. Failure to open the log aborts command startup; write failures
are reported when the command exits, never by printing into a live TUI.

Records include component, version, command, PID, and subsystem. Info covers store
and tunnel lifecycle and replay outcomes; debug adds connection attempts, persisted
captures/webhooks, and ACK progress. Failures and reconnect backoff are visible at
warn/error. Script exceptions and automatic forwarding outcomes also go to the
file instead of the terminal. Logs omit raw URLs, paths, headers, payloads, script
names/exception text, and credential values. Sequence/session IDs, status codes,
sizes and durations may be retained. Review diagnostic files before sharing them.

These are backend diagnostics, not browser console capture or a new desktop log
viewer.

## Graceful client shutdown (next release)

Closing the GUI, quitting the TUI, or signalling the TUI stops admission of new
app operations, cancels and joins the relay tunnel and its writer, and gives
accepted replay/forward/script/store operations a five-second shutdown budget.
On timeout those operations are cancelled; an unjoined worker keeps its database
open and shutdown returns an error instead of closing an in-use store. GUI and
TUI cleanup errors are recorded/returned rather than discarded. GUI operating-system
signal handling remains supplied by Wails; normal window closure uses the same
app shutdown path. SIGKILL and abrupt power loss cannot run graceful cleanup.

Interception shutdown stops new proxy/control API work and drains accepted HTTP
and CONNECT exchanges. Persistent CONNECT connections do not accept an unlimited
stream of new exchanges while draining: the current exchange can finish, and idle
or stalled tunnels are force-closed at the deadline. Forced cleanup cancels upstream
I/O and waits up to one additional second for handlers. Shell startup patches and
override wrappers are cleaned even if network draining times out; session-row
finalization uses its own one-second deadline. The CLI-owned store is closed only
after proxy/control API workers leave. Allow at least eight seconds for interception
cleanup before sending SIGKILL. Interrupted upstream requests may already have
performed side effects; Wiretap does not retry them automatically.

## Interception

| Command | Purpose |
| --- | --- |
| `wiretap intercept start` | Start proxy, local API, and an intercepted child shell |
| `wiretap intercept start --shell <kind>` | Select `bash`, `fish`, `powershell`, or `gitbash` |
| `wiretap intercept start --no-shell` | Run proxy and API without spawning a shell |
| `wiretap intercept attach` | Open another shell on the active interception session |
| `wiretap intercept attach --shell <kind>` | Attach using `bash`, `fish`, `powershell`, or `gitbash` |
| `wiretap intercept trust-ca` | Install the local CA into system trust; may require elevation |
| `wiretap intercept stop` | Remove Wiretap-managed shell startup blocks |

## Code export

```sh
wiretap export targets
wiretap export capture <capture-id> --as target[/client]
wiretap export webhook <project> <webhook-id> --as target[/client]
```

Examples:

```sh
wiretap export capture 42 --as shell/curl
wiretap export webhook new-project 7 --as javascript/fetch
wiretap export webhook new-project 7 --as python/requests
```

Omitting the client selects that target's default. Exported snippets reproduce the request side of the exchange and omit hop-by-hop and `Content-Length` headers.

## Relay registration

```sh
wiretap relay \
  --url https://relay.example.com \
  --admin-token TOKEN \
  register --name client-name --projects new-project,another-project --save
```

:::tip
`--projects` is optional. Use `projects add` later instead of re-registering.
:::

## This client's project commands

These use this client's saved credentials. `add` creates a new project with this client as its first subscriber; `remove` unsubscribes only this client and preserves the project and relay history:

```sh
wiretap relay --url https://relay.example.com projects add <path>
wiretap relay --url https://relay.example.com projects remove <path>
```

## Administrator commands

These require `--admin-token`:

```sh
wiretap relay clients list
wiretap relay clients get <client-id>
wiretap relay clients delete <client-id>

wiretap relay projects list
wiretap relay projects create <path> --client-id <client-id>
wiretap relay projects delete <path> --force
wiretap relay projects subscribe <path> --client-id <client-id> [--include-history]
wiretap relay projects unsubscribe <path> --client-id <client-id>
wiretap relay projects reclaim <path> --client-id <client-id> --force

wiretap relay webhooks list <project> [--after-seq <seq>] [--limit <n>]
wiretap relay webhooks replay <project> <seq> [--client-id <client-id>]
wiretap relay webhooks delete <project> <seq> [<seq>...]
wiretap relay webhooks delete <project> --through <seq>
wiretap relay webhooks delete <project> --all
```

New subscribers receive only future traffic unless `--include-history` is set. `projects reclaim` replaces every subscriber with the selected client; prefer `subscribe` and `unsubscribe` for ordinary changes.

The desktop GUI exposes client and project creation, client revocation, subscriber management, retained-history inspection, selected or complete history deletion, and project deletion.

## Relay server binary

```text
wiretap-relay -addr :8443 -db relay.db -admin-token TOKEN
```

The required environment variables are `WIRETAP_RELAY_ADDR`, `WIRETAP_RELAY_DB`, and `WIRETAP_ADMIN_TOKEN`.
