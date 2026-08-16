# wiretap

Inspect local HTTP traffic and receive public webhooks without exposing your development machine.

wiretap combines three workflows in one local tool:

- **Traffic capture** — launch an isolated shell whose HTTP and HTTPS requests pass through a recording proxy.
- **Webhook ingress** — receive webhooks through a self-hosted relay, including deliveries sent while your desktop is offline.
- **Payload transforms** — use local JavaScript to inspect, modify, or reject requests, responses, replays, and webhooks.

Use the desktop GUI, terminal UI, or CLI against the same local data.

## Install

Prebuilt release binaries are the recommended installation method. Until the release installer is published, follow the [installation guide](docs/INSTALLATION.md) for available manual and source-build options.

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

See the [interception guide](docs/INTERCEPTION.md) for what the generated shell script does, how TLS/CA trust is handled without touching your system trust store, and the PATH shims for git/curl/node.

Useful alternatives:

```sh
wiretap intercept start --shell fish
wiretap intercept start --no-shell
sudo wiretap intercept trust-ca
wiretap intercept stop
```

The local control API is available during interception:

```sh
curl http://127.0.0.1:9876/local/health
curl 'http://127.0.0.1:9876/local/captures?limit=50'
```

## Receive public webhooks

wiretap uses a self-hosted public relay. The desktop establishes an outbound WebSocket connection, so it does not need a public IP or an inbound firewall rule.

1. Deploy `wiretap-relay` by following the [hosting guide](docs/HOSTING.md).
2. Register the desktop with the relay:

   ```sh
   wiretap relay \
     --url https://relay.example.com \
     --admin-token YOUR_ADMIN_TOKEN \
     register --projects project-a --name laptop --save
   ```

3. Set the desktop tunnel endpoint in your configuration:

   ```yaml
   relay:
     url: wss://relay.example.com/tunnel
   ```

4. Start `wiretap gui` or `wiretap tui`, then send a webhook:

   ```sh
   curl -X POST https://relay.example.com/project-a/orders/created \
     -H 'Content-Type: application/json' \
     -d '{"order_id":"test-123"}'
   ```

The first URL segment identifies the registered project. Any remaining path is preserved for inspection and replay.

## Payload scripts

Create scripts from the GUI's **Transforms** section and attach one of these triggers:

| Trigger | Runs |
|---|---|
| `on_request` | Before intercepted traffic goes upstream |
| `on_response` | Before an intercepted response reaches its client |
| `on_replay` | Before a stored webhook is replayed locally |
| `on_webhook` | Before a relay webhook is stored locally |

Scripts execute locally and do not require Node.js. See the [scripting guide](docs/SCRIPTING.md) for the API, helpers, examples, and execution behavior — including an [end-to-end walkthrough](docs/SCRIPTING.md#end-to-end-walkthrough) from capturing a request to rewriting and rejecting it.

## Configuration

Run `wiretap config init` to create the platform-specific `config.yaml`. On Linux it is stored at `~/.config/wiretap/config.yaml`.

Every configuration key is documented in [`config.example.yaml`](config.example.yaml). Copy the settings you want to change; omitted settings retain their defaults.

Local state may contain request bodies, credentials, or personal data. Protect the wiretap configuration directory and any relay database accordingly.

## Stack

- Go
- SQLite
- Wails with Preact for the desktop GUI
- Bubble Tea for the terminal UI
- JavaScript payload transforms
