---
title: Manage the relay
description: Add or remove desktop projects and perform privileged client and ownership operations.
---

Wiretap separates day-to-day project changes from privileged relay administration.

## Manage this desktop's projects

After registration, add a project with the saved client identity:

```sh
wiretap relay --url https://relay.example.com projects add project-b
```

Remove an owned project only when its queued relay history can be deleted:

```sh
wiretap relay --url https://relay.example.com projects remove project-b --force
```

The GUI provides the same controls in **Settings → Relay connection** and reconnects the tunnel automatically after a change.

## Connect as a relay administrator

Open **Settings → Relay server**, enter the relay's HTTPS base URL and admin token, then connect. The workspace shows relay health, live desktop tunnel sessions, registered clients, and project ownership.

Enable **Remember this relay** to save a named profile. Wiretap stores its admin token in the operating-system credential store and keeps only profile metadata locally. If secure storage is unavailable, the connection remains temporary; there is no plaintext admin-token fallback.

From this workspace, an operator can:

- create credentials for another client without replacing this desktop's identity;
- revoke a client and delete its project bindings and queued histories;
- create a project for a registered client;
- move project ownership while preserving queued history;
- delete one project and its queued history.

Changes affecting the current desktop synchronize its saved project list and tunnel automatically.

## Use the CLI

List the server state:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN clients list
wiretap relay --url https://relay.example.com --admin-token TOKEN projects list
wiretap relay --url https://relay.example.com --admin-token TOKEN webhooks list project-a
```

Move a project to a different client:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  projects reclaim project-a --client-id CLIENT_ID --force
```

Replay a queued delivery from the relay:

```sh
wiretap relay --url https://relay.example.com --admin-token TOKEN \
  webhooks replay project-a 1
```

See [CLI reference](/reference/cli/) for the complete command map.
