---
title: Desktop identities and projects
description: Learn how relay registration, client credentials, shared projects, subscriptions, and delivery cursors fit together.
---

## A registration identifies one client

`wiretap relay register` creates a relay client ID and token. It can create initial project paths at the same time with `--projects`, and `--save` stores the identity for tunnel startup and ordinary project management.

To add a route later, use `wiretap relay projects add` with the saved identity instead of registering again:

```sh
wiretap relay projects add new-project
```

:::caution
Do not register again just to add a route. A second registration creates a second client identity.
:::

## A project is a durable public route

For this request:

```text
POST https://relay.example.com/new-project/orders/created
```

`new-project` selects the relay project. `/orders/created` is preserved as the webhook path.

A project can have multiple subscribed clients. The relay stores one copy of each webhook and delivers it independently to every subscriber. Each subscription has its own start sequence, acknowledgement cursor, and pending count, so one offline client does not advance another client's state.

The first client creates the project. Joining an existing project requires a relay administrator, which prevents a client from subscribing itself to a guessable path.

## Choose whether a new subscriber receives history

By default, a newly added subscriber starts after the latest retained sequence and receives only future traffic. A relay administrator can deliberately include retained history:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  projects subscribe new-project --client-id CLIENT_ID --include-history
```

This choice affects only the new subscription. Existing subscribers keep their current cursors.

## Unsubscribing is not deletion

The current client can remove its own subscriptions with its client credentials:

```sh
wiretap relay projects remove new-project
```

The project, other subscribers, and retained relay history remain. If the last subscriber leaves, the project becomes inactive and ingress returns not found until an administrator adds a subscriber again.

:::note
A relay administrator can delete the project and its retained history explicitly. Revoking a client removes that client's subscriptions but does not delete shared projects or history. The legacy `projects reclaim` command replaces every existing subscriber with one client and should be reserved for deliberate recovery or reassignment.
:::

## Two kinds of secret

| Secret | Used for | Storage behavior |
| --- | --- | --- |
| Relay admin token | Registration and `/admin/*` operations | Memory only, or OS keyring for an opted-in saved relay profile |
| Desktop client token | Tunnel authentication and the desktop's own project subscription changes | OS keyring when available; protected file fallback for unattended/headless use |
