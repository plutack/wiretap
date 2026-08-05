# Hosting `wiretap-relay`

`wiretap-relay` is the public, self-hosted half of wiretap. Run it on a server
with a stable DNS name. It accepts webhook ingress and keeps each webhook in
SQLite until the owning desktop client acknowledges it over the WebSocket
tunnel.

The relay serves plain HTTP. Put Caddy, nginx, or another TLS reverse proxy in
front of it so public traffic uses HTTPS and the desktop tunnel uses WSS.

## 1. Build and install

On the server, from a wiretap checkout:

```sh
go build -trimpath -ldflags "-s -w" -o wiretap-relay ./cmd/wiretap-relay
sudo install -m 0755 wiretap-relay /usr/local/bin/wiretap-relay
sudo useradd --system --home /var/lib/wiretap --shell /usr/sbin/nologin wiretap
sudo install -d -o wiretap -g wiretap -m 0750 /var/lib/wiretap
```

Generate a long random admin token. This token can register clients, inspect
stored webhooks, and change project ownership, so do not put it in shell
history, source control, a URL, or the desktop credentials file.

```sh
openssl rand -hex 32
```

Store the generated value in `/etc/wiretap-relay.env`:

```text
WIRETAP_ADMIN_TOKEN=replace-with-the-generated-token
```

Then restrict the file:

```sh
sudo chown root:root /etc/wiretap-relay.env
sudo chmod 0600 /etc/wiretap-relay.env
```

## 2. Run with systemd

Create `/etc/systemd/system/wiretap-relay.service`:

```ini
[Unit]
Description=wiretap webhook relay
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=wiretap
Group=wiretap
WorkingDirectory=/var/lib/wiretap
EnvironmentFile=/etc/wiretap-relay.env
ExecStart=/usr/local/bin/wiretap-relay -addr 127.0.0.1:8443 -db /var/lib/wiretap/relay.db
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/var/lib/wiretap

[Install]
WantedBy=multi-user.target
```

Enable it and confirm the private listener responds:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now wiretap-relay
curl http://127.0.0.1:8443/health
sudo journalctl -u wiretap-relay -n 50 --no-pager
```

Back up `/var/lib/wiretap/relay.db`. It contains client credentials, project
ownership, delivery cursors, and webhook payloads. Webhook bodies may contain
sensitive customer data; apply an appropriate retention and backup policy.

## 3. Terminate TLS with Caddy

Point a DNS `A`/`AAAA` record such as `relay.example.com` at the server. A
minimal Caddyfile is:

```caddyfile
relay.example.com {
    reverse_proxy 127.0.0.1:8443
}
```

Caddy obtains and renews the certificate and proxies WebSocket upgrades without
extra configuration. Only ports 80 and 443 need to be publicly reachable; keep
8443 bound to `127.0.0.1`.

After reloading Caddy, verify the public endpoint:

```sh
curl https://relay.example.com/health
```

A production reverse proxy should also set a request-body limit of at least
10 MiB, allow long-lived WebSocket connections on `/tunnel`, preserve the
request path, and avoid buffering tunnel traffic.

## 4. Register a desktop

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

## 5. Send and replay webhooks

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

## 6. Upgrades and recovery

1. Back up `relay.db`.
2. Replace `/usr/local/bin/wiretap-relay` with the new binary.
3. Run `sudo systemctl restart wiretap-relay`.
4. Check `/health` and the service journal.

Database migrations run automatically at startup and are idempotent. Keep the
old binary and database backup until the new process has started and a desktop
client has reconnected.

If the relay database is lost, existing desktop `client_token` values no longer
exist on the server. Register the desktop again and replace its saved
credentials. If the admin token is exposed, rotate the value in
`/etc/wiretap-relay.env` and restart the service; existing client tunnel tokens
remain valid.
