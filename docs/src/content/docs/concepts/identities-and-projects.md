---
title: Desktop identities and projects
description: Learn how relay registration, client credentials, projects, and ownership fit together.
---

## A registration identifies one desktop

`wiretap relay register` creates a relay client ID and token. With `--save`, Wiretap stores that identity for tunnel startup and ordinary project management.

Registration and project management are separate operations. Adding a project does not rotate the desktop's identity:

```sh
wiretap relay projects add project-b
```

Do not register again just to add a route. A second registration creates a second client identity.

## A project is a public route

For this request:

```text
POST https://relay.example.com/project-a/orders/created
```

`project-a` selects the owning desktop. `/orders/created` is preserved as the webhook path.

Each project currently has exactly one owner. Independent multi-subscriber delivery and per-subscriber cursors are not implemented.

## Removing and moving projects

An owner can remove its project with saved client credentials:

```sh
wiretap relay projects remove project-b --force
```

Removal deletes that project's queued relay-side webhook history. Deliveries already stored on the desktop remain local.

A relay administrator can move a project to another registered client while preserving its queued history. Deleting a client is broader: it deletes that client's project bindings and queued histories.

## Two kinds of secret

| Secret | Used for | Storage behavior |
| --- | --- | --- |
| Relay admin token | Registration and `/admin/*` operations | Memory only, or OS keyring for a named GUI relay profile |
| Desktop client token | Tunnel authentication and owner project changes | OS keyring when available; protected file fallback for unattended/headless use |

Keeping these roles separate limits how often the more powerful admin token is needed.
