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

`wiretap intercept start` opens a child shell whose supported HTTP clients use Wiretap's loopback proxy and local certificate authority.

## Inbound webhooks

```text
webhook sender ──HTTPS──► public relay ──outbound WSS──► desktop A
                              │       └──outbound WSS──► desktop B
                              ▼                              │
                         queued SQLite                  transforms
                                                            │
                                                            ▼
                                                       local SQLite
                                                            │
                                                            ▼
                                                      replay locally
```

Webhook traffic reaches the public HTTPS endpoint of your hosted `wiretap-relay` deployment. The relay stores each webhook once and forwards it to every client subscribed to the project. Each subscriber advances an independent acknowledgement cursor and catches up after reconnecting. Because every desktop initiates its own WSS tunnel, no inbound desktop port is exposed to the internet.

## Where does your data live

- Intercepted traffic, received webhooks, and transforms live in the desktop's local SQLite database.
- Queued webhook bodies, projects, subscriptions, delivery cursors, and client registrations live in the relay's SQLite database.
- Relay client tokens prefer the operating-system credential store and fall back to a credentials file that only your user account can read (mode `0600`). Relay-admin tokens are never written to disk as plaintext; a saved admin profile requires the operating-system keyring.


## Transform points

Transforms run in a local, sandboxed JavaScript runtime:

- `on_request` before an intercepted request goes upstream;
- `on_response` before the response returns to the intercepted client;
- `on_webhook` before a delivered webhook is stored locally;
- `on_replay` before a webhook or composed request is sent;
- `on_compose` only when selected to prepare a request draft.

Transforms have no filesystem or network API. See the [Transform API](/reference/transform-api/) for the exact data model and helpers.
