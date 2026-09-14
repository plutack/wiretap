---
title: Troubleshooting
description: Fix common installation, interception, relay, keyring, and large-payload issues.
---

## `wiretap gui` is unavailable or does not open

The installed binary may be the CLI/TUI build. Install a desktop release artifact or build with `make gui`. On Linux, confirm GTK 3 and WebKitGTK are installed.

Use `wiretap tui` to access the same local data without a graphical desktop.

## HTTPS requests fail in the intercepted shell

Confirm the command runs inside the child shell opened by `wiretap intercept start`. Some tools ignore standard CA variables; see [TLS trust](/guides/intercept-traffic/#tls-trust).

If the client uses certificate pinning, interception will fail by design. Exclude that host or run the client outside the intercepted shell.

## Requests are not captured

- Check that the command is running inside the intercepted child shell.
- Inspect its `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` values.
- A hostname covered by `NO_PROXY` bypasses Wiretap.
- Some applications ignore proxy environment variables or use direct sockets.
- Confirm the proxy address in `intercept.proxy_addr` is available.

## The local API is unreachable

The local API runs only while `wiretap intercept start` is active. With defaults:

```sh
curl http://127.0.0.1:9876/local/health
```

Check `intercept.local_api_addr` if you changed it.

## A webhook does not arrive

1. Check `https://relay.example.com/health`.
2. Confirm the desktop config uses `wss://relay.example.com/tunnel`.
3. Confirm the GUI or TUI reports the tunnel as connected.
4. Confirm the first URL segment exactly matches a project this desktop subscribes to.
5. Verify the reverse proxy permits WebSocket upgrades on `/tunnel`.
6. Check that the relay volume is writable and persistent.

Queued webhooks arrive after reconnection. If the relay database was replaced, register the desktop again.

## Adding a project asks for credentials or fails

`projects add` uses the desktop credentials saved by `register --save`. Register once if that file or keyring entry is missing. Pass the relay's HTTPS base URL with `--url`; the command derives ingress from the configured WSS URL only when possible.

Do not re-register merely to add a project, because that creates a new client identity.

## A saved relay-admin profile cannot find its token on Linux

Wiretap uses the Secret Service `default` collection. Make sure the desktop keyring is unlocked and its default alias resolves to the collection where the profile was saved.

Profiles created by v0.2.11 or v0.2.12 may reference a different collection. Reconnect with the relay URL and admin token, enable **Remember this relay**, and save it again with v0.2.13 or later.

## A large body is truncated in the viewer

This is expected. Wiretap initially fetches at most 256 KiB per captured request or response body and expands the preview progressively. Use **Show more**, **Save**, or **Copy all** when you deliberately need more data.

Formatting and syntax highlighting apply only to complete bodies no larger than 100 KiB. Larger previews render as plain text to keep the UI responsive.

## A composed response is truncated

The request composer retains at most 2 MiB of a response. Use a dedicated HTTP client when you need to download or inspect a larger result in full.

## A transform did not produce the expected output

- Confirm it is enabled and uses the correct trigger.
- Lower priority numbers run first.
- Run the unsaved program in the GUI test bench and inspect console output and errors.
- `on_request` and `on_response` require interception.
- `on_webhook` and `on_replay` require the GUI or TUI relay tunnel.
- `on_compose` runs only after explicit recipe selection.

Thrown errors do not stop later scripts; `reject()` does.
