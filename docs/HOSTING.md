# Hosting `wiretap-relay`

`wiretap-relay` is the public, self-hosted half of wiretap. Run it on a server
with a stable DNS name. It accepts webhook ingress and keeps each webhook in
SQLite until the owning desktop client acknowledges it over the WebSocket
tunnel.

The relay is a single static Go binary that serves plain HTTP. TLS is
terminated by a reverse proxy in front of it (Coolify's built-in Caddy if you
use Coolify, or your own Caddy/nginx) so public traffic uses HTTPS and the
desktop tunnel uses WSS.

Configuration is environment-driven, which is what container platforms expect:

| Variable | Default | Purpose |
|---|---|---|
| `WIRETAP_ADMIN_TOKEN` | — (required) | Admin token for `/register` and `/admin/*` |
| `WIRETAP_RELAY_ADDR` | `:8443` | Listen address |
| `WIRETAP_RELAY_DB` | `relay.db` | SQLite database path |

The same knobs exist as `-addr`, `-db`, and `-admin-token` flags; flags win
over env vars. An unauthenticated `GET /health` returns the running version
and is meant for health checks.

## 1. Deploy on Coolify

Coolify already runs Caddy as its proxy, so TLS certificates and WebSocket
proxying are handled for you — including the `/tunnel` WebSocket the desktop
clients use.

1. **Create the app.** In your Coolify project, add a new resource → *Dockerfile*
   from a Git repository, pointing at this repo. Coolify builds the repo's
   root `Dockerfile` (multi-stage, distroless, non-root).

2. **Configure the service:**
   - **Port:** expose the container's `8443` and mark it as the health-check
     port (the image has no explicit healthcheck; point Coolify's at
     `GET /health`, or leave it off).
   - **Environment variable:** add `WIRETAP_ADMIN_TOKEN` as a *secret*. This
     token can register clients, inspect stored webhooks, and change project
     ownership — generate it with `openssl rand -hex 32` and never put it in
     source control or a URL.
   - **Persistent storage:** add a volume mounted at `/data`. The image stores
     its SQLite database at `/data/relay.db` by default. Without a volume the
     database is lost on every redeploy.

3. **Attach a domain.** Give the service a FQDN (e.g.
   `relay.example.com`). Coolify's Caddy obtains and renews the certificate and
   proxies WebSockets without extra configuration.

4. **Verify:**

   ```sh
   curl https://relay.example.com/health
   ```

Desktop clients connect to `wss://relay.example.com/tunnel` (see
[Register a desktop](#3-register-a-desktop) below).

### Upgrades

Redeploy from the new release (Coolify rebuilds the image). Database
migrations run automatically at startup and are idempotent. Back up the
`/data` volume first — it contains client credentials, project ownership,
delivery cursors, and webhook payloads, which may hold sensitive customer
data.

If the relay database is lost, existing desktop `client_token` values no
longer exist on the server: register the desktop again and replace its saved
credentials. If the admin token is exposed, rotate the secret and redeploy;
existing client tunnel tokens remain valid.

## 2. Run behind your own reverse proxy

Any host that can run a static binary or a Docker container works. The only
requirements: TLS termination, preserved request paths, WebSocket upgrades on
`/tunnel`, a request-body limit of at least 10 MiB, and no buffering of tunnel
traffic.

A minimal Caddyfile:

```caddyfile
relay.example.com {
    reverse_proxy 127.0.0.1:8443
}
```

Or with Docker directly:

```sh
docker build -t wiretap-relay .
docker run -d --name wiretap-relay \
  -e WIRETAP_ADMIN_TOKEN=$(openssl rand -hex 32) \
  -p 127.0.0.1:8443:8443 \
  -v wiretap-relay-data:/data \
  wiretap-relay
```

Keep the listening port bound to `localhost` and let the proxy own 80/443.

## 3. Register a desktop

On the desktop, initialize the config and register one or more project paths:

```sh
wiretap config init
wiretap relay \
  --url https://relay.example.com \
  --admin-token replace-with-the-admin-token \
  register --projects project-a,project-b --name laptop --save
```

`--save` writes `client_id`, `client_token`, and the claimed projects to
`~/.config/wiretap/relay-credentials.json` with mode `0600`. It intentionally
does not save the more privileged relay admin token.

Set the tunnel endpoint in `~/.config/wiretap/config.yaml`:

```yaml
relay:
  url: wss://relay.example.com/tunnel
  creds_file: ""
```

The current configuration has one `relay.url` field even though admin commands
need an HTTPS base URL and the app needs a WSS endpoint. Use `--url
https://relay.example.com` for relay administration as shown above; reserve the
configured value for `wss://relay.example.com/tunnel`.

Start `wiretap gui` or `wiretap tui`. The status should show the connected
project paths. The desktop always dials outward, so no inbound desktop port or
third-party tunneling service is required.

## 4. Send and replay webhooks

A sender posts to the claimed project path. Any path after the project segment
is preserved:

```sh
curl -X POST https://relay.example.com/project-a/orders/created \
  -H 'Content-Type: application/json' \
  -d '{"order_id":"test-123"}'
```

If the desktop is offline, the relay stores the webhook and streams it after
the client reconnects. The desktop acknowledges a cursor per project, so
redelivery is safe and local inserts are idempotent.

Useful administration commands:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN projects list
wiretap relay --url https://relay.example.com --admin-token TOKEN clients list
wiretap relay --url https://relay.example.com --admin-token TOKEN webhooks list project-a
wiretap relay --url https://relay.example.com --admin-token TOKEN webhooks replay project-a 1
```
