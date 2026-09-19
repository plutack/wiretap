---
title: Host the relay
description: Deploy wiretap-relay with persistent storage, TLS, WebSocket proxying, and a protected admin token.
---

`wiretap-relay` is Wiretap's public, self-hosted ingress. It accepts webhooks, persists them in SQLite, and delivers them through an outbound desktop WebSocket tunnel.

## Requirements

The relay is a static Go binary that serves plain HTTP. Put it behind an HTTPS reverse proxy that:

- preserves request paths;
- supports WebSocket upgrades on `/tunnel`;
- allows request bodies of at least 10 MiB;
- does not buffer tunnel traffic.

Persist the SQLite database. Losing it removes registrations, projects, subscriptions, queued deliveries, and delivery cursors.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `WIRETAP_ADMIN_TOKEN` | required | Authorizes `/register` and `/admin/*` |
| `WIRETAP_RELAY_ADDR` | `:8443` | HTTP listen address |
| `WIRETAP_RELAY_DB` | `relay.db` | SQLite database path |

Equivalent flags are `-admin-token`, `-addr`, and `-db`; flags win over environment variables. `GET /health` is unauthenticated.

Generate a strong admin token:

```sh
openssl rand -hex 32
```

Store it as a deployment secret. It can register and revoke clients, manage project subscriptions, inspect or delete queued webhooks, and replay retained deliveries.

## Run with Docker

Prebuilt images are published to GitHub Container Registry as `ghcr.io/plutack/wiretap-relay`. This is the quickest way to get a relay running:

```sh
docker run -d --name wiretap-relay \
  -e WIRETAP_ADMIN_TOKEN=<secure-token> \
  -p 127.0.0.1:8443:8443 \
  -v wiretap-relay-data:/data \
  ghcr.io/plutack/wiretap-relay:latest
```

Images are published for each release: `latest` tracks the newest, while a version tag such as `0.3.0` lets you stay on one you've tried. They are built for `linux/amd64`.

To build the image from the repository Dockerfile instead:

```sh
docker build -t wiretap-relay .
docker run -d --name wiretap-relay \
  -e WIRETAP_ADMIN_TOKEN=<secure-token> \
  -p 127.0.0.1:8443:8443 \
  -v wiretap-relay-data:/data \
  wiretap-relay
```

In both commands, replace `<secure-token>` with the admin token you generated above.

:::note
The `wiretap-relay-data` volume holds the SQLite database, so your registrations and queued webhooks survive a restart.
:::

### Docker Compose

The repository ships a `docker-compose.yml` with the persistent volume already wired up. Generate an admin token once, keep it in a `.env` file beside the compose file (it is gitignored), and start the service:

```sh
printf 'WIRETAP_ADMIN_TOKEN=%s\n' "$(openssl rand -hex 32)" > .env
docker compose up -d
```

The named `relay-data` volume is mounted at `/data`, which is where the relay keeps its SQLite database. Stopping, recreating, or upgrading the container leaves your registrations, subscriptions, delivery cursors, and any webhooks still queued for an offline desktop intact. Remove the volume only when you intend to discard the relay's history.

The compose file pins the image to a release and binds `127.0.0.1:8443`, matching the reverse-proxy setup below. Change the port mapping to `8443:8443` when the proxy runs in a different container or host.

Keep the relay bound to loopback and let the reverse proxy own public ports 80 and 443.

## Add TLS with Caddy

```text
relay.example.com {
    reverse_proxy 127.0.0.1:8443
}
```

```sh
curl https://relay.example.com/health
```

## Deploy with Coolify

1. Add a Docker image application using `ghcr.io/plutack/wiretap-relay:latest`, or a Dockerfile application from the Wiretap repository.
2. Expose container port `8443`.
3. Add `WIRETAP_ADMIN_TOKEN` as a secret.
4. Mount persistent storage at `/data`.
5. Attach a public domain and point the health check at `/health`.

Coolify's proxy handles HTTPS and WebSocket forwarding.

## Upgrade safely

Back up the persistent volume, then deploy the new relay image. Database migrations run transactionally at startup and each migration is recorded after it succeeds.

Upgrading to v0.3.0 converts each existing project owner into the project's first subscriber. Existing project paths, queued webhooks, sequence numbers, and acknowledgement cursors are preserved. Upgrade the relay before using the new subscription and retention-management commands.

If the relay database is lost, register desktops again because their existing client tokens no longer exist server-side. If only the admin token is exposed, rotate it and redeploy; existing client tunnel tokens remain valid.

Continue with [Receive your first webhook](/getting-started/first-webhook/).
