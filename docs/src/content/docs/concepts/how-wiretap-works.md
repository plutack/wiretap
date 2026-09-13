---
title: How Wiretap works
description: Understand local interception, public webhook ingress, storage, replay, and transforms.
---

Wiretap joins two traffic paths in one local history.

## Outbound traffic

```text
command in child shell
        │
        ▼
local MITM proxy ── transforms ──► destination server
        │                              │
        └──── request + response ◄─────┘
                       │
                       ▼
                 local SQLite
```

`wiretap intercept start` opens a child shell whose supported HTTP clients use Wiretap's loopback proxy and local certificate authority. Captures are written to the same SQLite database read by the GUI and TUI.

## Inbound webhooks

```text
webhook sender ──HTTPS──► public relay ──outbound WSS tunnel──► desktop
                              │                                  │
                         queued SQLite                       transforms
                                                                 │
                                                            local SQLite
                                                                 │
                                                           replay locally
```

The relay owns the public endpoint. It queues a delivery until the project owner's desktop acknowledges it. The desktop initiates the tunnel, so it does not expose a port to the internet.

## Where data lives

- Intercepted traffic, received webhooks, cursors, and transforms live in the desktop's local SQLite database.
- Queued webhook bodies, project ownership, and client registrations live in the relay's SQLite database.
- Relay client and relay-admin tokens prefer the operating-system credential store. Headless desktop clients can fall back to a mode-`0600` credentials file.

Treat both databases as sensitive: they may contain headers, credentials, personal data, and complete payloads.

## Transform points

Transforms run in a local, sandboxed JavaScript runtime:

- `on_request` before an intercepted request goes upstream;
- `on_response` before the response returns to the intercepted client;
- `on_webhook` before a delivered webhook is stored locally;
- `on_replay` before a webhook or composed request is sent;
- `on_compose` only when selected to prepare a request draft.

Transforms have no filesystem or network API. See the [Transform API](/reference/transform-api/) for the exact data model and helpers.
