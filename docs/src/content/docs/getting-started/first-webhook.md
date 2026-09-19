---
title: Receive your first webhook
description: Register a desktop, claim a project path, and receive a webhook through your Wiretap relay.
---

This walkthrough assumes you have [deployed `wiretap-relay`](/guides/host-relay/) at a public HTTPS address such as `https://relay.example.com`.

## 1. Register this desktop

Registration creates one desktop identity. Run it once and save the returned client credentials:

```sh
wiretap relay \
  --url https://relay.example.com \
  --admin-token YOUR_ADMIN_TOKEN \
  register --name client-name --save
```

The admin token is needed for registration, but not for ordinary project changes. Do not place it in a URL, shell history, or committed config file.

## 2. Claim a project path

Use the saved desktop credentials to add a project:

```sh
wiretap relay --url https://relay.example.com projects add new-project
```

`new-project` becomes the first segment of the public webhook URL. This desktop is the project's first subscriber; a relay administrator can add more subscribers later.

## 3. Configure the outbound tunnel

Open the configuration file created by `wiretap config init` and set:

```yaml
relay:
  url: wss://relay.example.com/tunnel
```

The configured value is a WebSocket URL. Relay administration commands use the HTTPS base URL passed with `--url`.

## 4. Connect Wiretap

Start the desktop or terminal UI:

```sh
wiretap gui
```

![The Settings Relay connection pane, showing a configured tunnel URL, the registered client identity, and where the client token is stored.](/screenshots/06-settings.png)

The status area should show a connected relay and `new-project`. The desktop dials outward, so it needs no public IP or inbound firewall rule.

## 5. Send a webhook

From any machine that can reach the relay:

```sh
curl -X POST https://relay.example.com/new-project/orders/created \
  -H 'Content-Type: application/json' \
  -H 'X-Test-Event: order.created' \
  -d '{"order_id":"test-123"}'
```

Open **Webhooks** and select the delivery. Wiretap preserves everything after the project segment, so the recorded path is `/orders/created`.

![The Ingress tab listing delivered webhooks newest first, with the subscribed project sources and recording sessions in the sidebar.](/screenshots/01-ingress.png)

## 6. Replay it locally

In the webhook detail, choose **Replay**, enter your local endpoint such as `http://127.0.0.1:8080/webhooks`, and send it again. You can set `relay.forward_url` to forward every arriving webhook automatically.

![A webhook detail pane showing the recorded method, route, headers, and request body, with the replay target field below.](/screenshots/02-webhook-detail.png)

:::tip[Try the offline queue]
Close the desktop, send another webhook, then reconnect. The relay keeps the delivery in SQLite until the desktop acknowledges it.
:::

For adding or removing subscribers and managing retained history later, see [Manage the relay](/guides/manage-relay/).
