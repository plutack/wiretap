---
title: CLI reference
description: Wiretap command groups and common flags.
---

Run `wiretap <command> --help` for the flags shipped by your installed version.

## Core commands

| Command | Purpose |
| --- | --- |
| `wiretap config init` | Create the default configuration file |
| `wiretap gui` | Open the desktop dashboard; requires a GUI-enabled build |
| `wiretap tui` | Open the terminal dashboard |
| `wiretap version` | Print the embedded version |
| `wiretap completion <shell>` | Generate shell completion |

`wiretap config init --force` replaces an existing config file.

## Interception

| Command | Purpose |
| --- | --- |
| `wiretap intercept start` | Start proxy, local API, and an intercepted child shell |
| `wiretap intercept start --shell <kind>` | Select `bash`, `fish`, `powershell`, or `gitbash` |
| `wiretap intercept start --no-shell` | Run proxy and API without spawning a shell |
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
