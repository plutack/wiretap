# wiretap

Inspect local HTTP traffic and receive public webhooks without exposing your development machine.

wiretap combines three workflows in one local tool:

- **Traffic capture** — launch an isolated shell whose HTTP and HTTPS requests pass through a recording proxy.
- **Webhook ingress** — receive webhooks through a self-hosted relay, including deliveries sent while your desktop is offline.
- **Payload transforms** — use local JavaScript to inspect, modify, or reject requests, responses, replays, and webhooks.
- **Code export** — turn any stored capture or webhook into a ready-to-run snippet (curl, fetch, python-requests, go, and ~15 more) via an embedded [httpsnippet](https://github.com/Kong/httpsnippet) engine — no Node.js required.
- **Request composer** — author HTTP requests, import JSON payloads or request envelopes, apply replay transforms, and inspect bounded responses from the GUI.
- **Portable transforms** — import, duplicate, inspect, test, and export versioned transform files for backup or sharing.

Use the desktop GUI, terminal UI, or CLI against the same local data.

## Install

Prebuilt release binaries are the recommended installation method. Follow the [installation guide](docs/src/content/docs/getting-started/install.md) for Linux, Windows, verification, and source-build options.

After installation:

```sh
wiretap config init
wiretap gui
```

Configuration is optional for local traffic capture; wiretap uses safe loopback defaults when no config file exists.

## Capture local traffic

```sh
wiretap intercept start
```

wiretap starts a local recording proxy and opens a child shell with the required proxy and CA environment. Run `curl`, `git`, Node, or another HTTP-aware command inside that shell. Exit the child shell to stop the interception session.

See the [interception guide](docs/src/content/docs/guides/intercept-traffic.md) for child-shell behavior, scoped TLS trust, and cleanup.

Useful alternatives:

```sh
wiretap intercept start --shell fish
wiretap intercept start --no-shell
wiretap intercept attach
wiretap intercept attach --shell fish
sudo wiretap intercept trust-ca
wiretap intercept stop
```

While an interception session is active, run `wiretap intercept attach` in
other terminals to open additional shells on the same proxy and capture
session. Exiting an attached shell does not stop the owner session.

The local control API is available during interception:

```sh
curl http://127.0.0.1:9876/local/health
curl 'http://127.0.0.1:9876/local/captures?limit=50'
```

## Export as code

Any stored capture or webhook can be exported as a runnable snippet, from the
GUI (“Export as code” in the detail pane) or the CLI:

```sh
wiretap export targets                      # list languages/clients
wiretap export capture 42 --as shell/curl
wiretap export webhook new-project 7 --as python/requests
```

`--as` takes `target[/client]`; omitting the client uses the target's default.
Snippets reproduce the request half of the exchange (hop-by-hop and
`Content-Length` headers are dropped so the generated code recomputes them).
Webhook exports target the relay's public ingress URL derived from your
configured tunnel endpoint.

## Compose and import requests

Open **Compose** in the GUI to send an arbitrary HTTP request. The composer
supports method, URL, headers, text/JSON bodies, optional `on_replay`
transforms, and response inspection with status, headers, body size, and
duration. An existing webhook or captured request can be opened directly in
the composer from its detail pane.

For provider logs and other non-request-shaped input, switch to **Source
recipe**. An enabled `on_compose` script can extract nested payloads and
headers, filter transport noise, and build the URL, method, headers, and body.
Wiretap then opens the result as a normal editable draft; recipe application
does not send it. Recipes are user-authored and selected per use, rather than
hard-coded or run globally.

Importing a normal `.json` file uses that document as the request body and
defaults to `POST` with `Content-Type: application/json`. A request envelope
can set the complete request instead:

```json
{
  "method": "POST",
  "url": "http://127.0.0.1:8080/webhook",
  "headers": {
    "Content-Type": ["application/json"],
    "X-Test-Event": ["order.created"]
  },
  "body": {"order_id": "test-123"},
  "apply_transforms": true
}
```

Only absolute `http://` and `https://` destinations are accepted. Responses
are capped to a 2 MiB preview so an unexpectedly large endpoint response does
not freeze the desktop UI.

## Receive public webhooks

wiretap uses a self-hosted public relay. The desktop establishes an outbound WebSocket connection, so it does not need a public IP or an inbound firewall rule.

1. Deploy `wiretap-relay` by following the [hosting guide](docs/src/content/docs/guides/host-relay.md).
2. Register the desktop with the relay:

   ```sh
   wiretap relay \
     --url https://relay.example.com \
     --admin-token YOUR_ADMIN_TOKEN \
     register --name laptop --save
   wiretap relay --url https://relay.example.com projects add new-project
   ```

3. Set the desktop tunnel endpoint in your configuration:

   ```yaml
   relay:
     url: wss://relay.example.com/tunnel
   ```

4. Start `wiretap gui` or `wiretap tui`, then send a webhook:

   ```sh
   curl -X POST https://relay.example.com/new-project/orders/created \
     -H 'Content-Type: application/json' \
     -d '{"order_id":"test-123"}'
   ```

The first URL segment identifies the registered project. Any remaining path is preserved for inspection and replay.

Registration creates a desktop identity and credentials. To create another
project later without replacing that identity, use:

```sh
wiretap relay projects add another-project
```

`relay register` also accepts no `--projects`; initial project flags are kept
as a setup convenience. `wiretap relay projects remove another-project` removes only
this desktop's subscription. The project and retained relay history remain.

Each project can deliver to multiple subscribed clients. Every subscriber has
an independent acknowledgement cursor and offline catch-up stream. A relay
administrator controls who can join an existing project and can choose whether
a new subscriber receives retained history or only future traffic.

### TUI dashboard

`wiretap tui` is the terminal counterpart of the GUI: three tabs (Ingress, Traffic, Transforms) over the same local store, with vim-style navigation (`j`/`k`, `g`/`G`, `h`/`l` paging).

| Key | Action |
|---|---|
| `1`/`2`/`3`, `tab` | Switch Ingress / Traffic / Transforms |
| `enter` | Open the selected webhook or capture (headers + body) |
| `m` | Cycle body view mode: auto → text → raw → hex |
| `tab` (detail) | Jump between a capture's request and response halves |
| `/` | Fuzzy-search the visible list; `esc` clears |
| `f` / `F` | Cycle method / status-family lens (Traffic) |
| `S` | Cycle interception-session filter (Traffic) |
| `c` | Clear all lens filters |
| `p` | Pause/resume the live feed (shows a backlog count) |
| `r` | Replay the selected webhook to a target URL (prefilled with `relay.forward_url`) |
| `e` | Export the selected row as code (language → client, `y` copies via OSC 52) |
| `space` | Enable/disable a transform script |
| `q` | Quit |

The `tui.theme` config key (`dark`, the default, or `light`) selects the color palette.

## Payload scripts

Create scripts from the GUI's **Transforms** section and attach one of these triggers:

| Trigger | Runs |
|---|---|
| `on_request` | Before intercepted traffic goes upstream |
| `on_response` | Before an intercepted response reaches its client |
| `on_replay` | Before a stored webhook is replayed locally |
| `on_webhook` | Before a relay webhook is stored locally |
| `on_compose` | When explicitly selected as a source recipe in Compose |

Scripts execute locally and do not require Node.js. See the [transform guide](docs/src/content/docs/guides/transforms.md) for the workflow and the [Transform API](docs/src/content/docs/reference/transform-api.md) for globals, helpers, ordering, and sandbox behavior.

## Configuration

Run `wiretap config init` to create the platform-specific `config.yaml`. On Linux it is stored at `~/.config/wiretap/config.yaml`.

A relay administrator can hand another user a versioned client file. Import it
without needing the relay admin token:

```sh
wiretap config import ./wiretap-client.json
```

Importing a different identity requires `--force`. The token is moved into the
operating-system keyring when available, and the source file should be deleted
after the secure handoff is complete.

Every configuration key is documented in [`config.example.yaml`](config.example.yaml). Copy the settings you want to change; omitted settings retain their defaults.

Alternatively, use the GUI: the gear icon in the top bar opens a Settings
screen that edits the same `config.yaml` (relay endpoint, interception
addresses, storage path, TUI theme, desktop window title bar) and performs
relay registration — the
equivalent of `wiretap relay register --save` — without the CLI. The admin
token is used once for registration unless the operator separately saves a
named relay-admin profile.

Settings is organized by workflow: **Relay connection**, **Capture and
delivery**, **Interface**, and a separate **Relay server** workspace for
operators. Relay administrators can create or revoke clients, create or delete
projects, manage subscribers, and inspect or delete retained webhooks. Named relay profiles can securely retrieve their
admin tokens from macOS Keychain, Windows Credential Manager, or Secret
Service. Desktop project changes preserve the existing client
identity and reconnect automatically. Wiretap also stores the desktop's
long-lived client token in a separate system-keyring entry when one is
available. On headless systems it automatically falls back to the private
mode-`0600` credentials file, with no storage setting to configure.
On Linux, Wiretap follows Secret Service's configured `default` alias and
stores its entries in that collection; it does not create a separate keyring
collection for the application.
Enter the relay URL and admin token to inspect health, manage registered
clients, create portable client credentials, manage shared project subscriptions,
and control relay-side webhook retention.
New client credentials can be downloaded as a JSON handoff file. The recipient
can import it from **Relay connection** or with `wiretap config import`; the GUI
reconnects only its background tunnel and does not restart Wiretap.
For temporary connections, the admin token remains in memory only and is
cleared when you leave the workspace. Saved profiles retrieve it from the
system keyring when needed. Creating a client there does not replace this
desktop's saved registration.

The Interface workspace also provides system, Wiretap light/dark, Nord, and
Catppuccin palette presets. Themes apply immediately to the complete workbench,
CodeMirror editor, and bounded payload syntax display. See the
[desktop customization guide](docs/src/content/docs/guides/desktop-themes.md) for themes, density, title bars, and bounded syntax highlighting.

Local state may contain request bodies, credentials, or personal data. Protect the wiretap configuration directory and any relay database accordingly.

## Stack

- Go
- SQLite
- Wails with Preact for the desktop GUI
- Bubble Tea for the terminal UI
- JavaScript payload transforms
