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

Persist the SQLite database. Losing it removes registrations, project ownership, queued deliveries, and delivery cursors.

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

Store it as a deployment secret. It can register and revoke clients, inspect queued webhooks, and change project ownership.

## Run with Docker

The repository Dockerfile builds a distroless, non-root relay image:

```sh
docker build -t wiretap-relay .
docker run -d --name wiretap-relay \
  -e WIRETAP_ADMIN_TOKEN=replace-with-a-secret \
  -p 127.0.0.1:8443:8443 \
  -v wiretap-relay-data:/data \
  wiretap-relay
```

Keep the relay bound to loopback and let the reverse proxy own public ports 80 and 443.

## Add TLS with Caddy

```text
relay.example.com {
    reverse_proxy 127.0.0.1:8443
}
```

Caddy obtains the certificate and proxies the WebSocket upgrade. Verify the deployment:

```sh
curl https://relay.example.com/health
```

## Deploy with Coolify

1. Add a Dockerfile application from the Wiretap repository.
2. Expose container port `8443`.
3. Add `WIRETAP_ADMIN_TOKEN` as a secret.
4. Mount persistent storage at `/data`.
5. Attach a public domain and point the health check at `/health`.

Coolify's proxy handles HTTPS and WebSocket forwarding.

## Upgrade safely

Back up the persistent volume, then deploy the new relay image. Database migrations run at startup and are idempotent.

If the relay database is lost, register desktops again because their existing client tokens no longer exist server-side. If only the admin token is exposed, rotate it and redeploy; existing client tunnel tokens remain valid.

Continue with [Receive your first webhook](/getting-started/first-webhook/).
