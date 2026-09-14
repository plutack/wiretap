---
title: Manage the relay
description: Manage desktop subscriptions, shared projects, clients, and retained webhook history.
---

Wiretap separates a desktop's own subscription changes from privileged relay administration.

## Manage this desktop's projects

After registration, create a new project with the saved client identity:

```sh
wiretap relay --url https://relay.example.com projects add new-project
```

Remove only this desktop's subscription:

```sh
wiretap relay --url https://relay.example.com projects remove new-project
```

The project and retained history remain on the relay. Other subscribed clients continue receiving deliveries. The GUI provides the same controls in **Settings → Relay connection** and reconnects the tunnel automatically after a change.

Client credentials cannot join a project that another client already subscribes to. Ask a relay administrator to add that subscription.

## Connect as a relay administrator

Open **Settings → Relay server**, enter the relay's HTTPS base URL and admin token, then connect. The workspace shows relay health, live desktop tunnel sessions, registered clients, projects, subscriber cursors, pending counts, and retained webhook totals.

Enable **Remember this relay** to save a named profile. Wiretap stores its admin token in the operating-system credential store and keeps only profile metadata locally. If secure storage is unavailable, the connection remains temporary; there is no plaintext admin-token fallback.

From this workspace, an operator can:

- create credentials for another client without replacing this desktop's identity;
- revoke a client and remove its subscriptions without deleting shared projects or history;
- create a project with an initial subscriber;
- add or remove subscribers and choose whether a new subscriber receives retained history;
- inspect retained webhook metadata in bounded pages;
- delete selected webhooks or all retained history for one project;
- delete a project and all its subscriptions and retained history.

Changes affecting the current desktop synchronize its saved project list and tunnel automatically.

## Use the CLI

List the server state and retained history:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN clients list
wiretap relay --url https://relay.example.com --admin-token TOKEN projects list
wiretap relay --url https://relay.example.com --admin-token TOKEN webhooks list new-project
```

Create a project, then add another subscriber:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  projects create new-project --client-id FIRST_CLIENT_ID
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  projects subscribe new-project --client-id SECOND_CLIENT_ID
```

Pass `--include-history` to `projects subscribe` only when the new subscriber should receive retained deliveries. Without it, the subscription begins after the newest existing sequence.

Remove a subscriber without deleting the project:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  projects unsubscribe new-project --client-id CLIENT_ID
```

Replay a retained delivery to every connected subscriber, or target one client:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  webhooks replay new-project 1
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  webhooks replay new-project 1 --client-id CLIENT_ID
```

## Delete retained webhooks

Delete selected sequences:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  webhooks delete new-project 10 11 15
```

Or delete an inclusive range or the complete retained history:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  webhooks delete new-project --through 100
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  webhooks delete new-project --all
```

Choose exactly one deletion mode. A selected-sequence request accepts at most 500 sequence numbers. Deletion changes relay retention only; webhooks already stored on desktops remain local.

:::caution[Reclaim replaces every subscriber]
`projects reclaim <path> --client-id <client-id> --force` is retained for compatibility and recovery. It removes every current subscription, adds only the replacement client, and keeps the relay's retained history. The replacement starts after the newest existing sequence, so it does not receive the retained backlog. Prefer explicit `subscribe` and `unsubscribe` operations for normal management.
:::

See [CLI reference](/reference/cli/) for the complete command map.
