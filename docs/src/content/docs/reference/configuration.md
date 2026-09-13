---
title: Configuration
description: Reference for the desktop config file, defaults, paths, and GUI-managed preferences.
---

Create the platform-specific configuration file with:

```sh
wiretap config init
```

On Linux, the default is `~/.config/wiretap/config.yaml`. Empty values use platform defaults. Paths may be absolute or relative to the process working directory; prefer absolute paths outside the Wiretap config directory.

## Complete example

```yaml
relay:
  # Desktop WebSocket tunnel. Empty disables relay connectivity.
  url: ""
  # Example: wss://relay.example.com/tunnel

  # Automatically replay each received webhook to this local URL.
  forward_url: ""
  # Example: http://127.0.0.1:8080/webhooks

  # Registration credentials. Empty uses relay-credentials.json
  # in the Wiretap config directory.
  creds_file: ""

store:
  # Local captures, webhooks, cursors, and transforms.
  # Empty uses wiretap.db in the Wiretap config directory.
  path: ""

tui:
  theme: dark

gui:
  # auto | always | never
  native_titlebar: auto

intercept:
  proxy_addr: 127.0.0.1:8888
  local_api_addr: 127.0.0.1:9876
  # bash | fish | powershell | gitbash; empty auto-detects
  shell: ""
```

## Relay settings

### `relay.url`

The WebSocket endpoint used by the desktop tunnel. This is normally `wss://relay.example.com/tunnel`, not the HTTPS base URL accepted by `wiretap relay --url`.

### `relay.forward_url`

When set, every arriving webhook is stored and then replayed to this local URL. Enabled `on_replay` transforms run before forwarding.

### `relay.creds_file`

Registration metadata and the desktop client credential reference. When empty, Wiretap uses `relay-credentials.json` beside the config file. The client token prefers the OS keyring and falls back to this file with mode `0600` on a headless system.

## Local storage

### `store.path`

SQLite database containing captures, delivered webhooks, relay cursors, and transform programs. Protect this file as sensitive data.

## Interface settings

### `tui.theme`

The terminal UI's built-in palette. The supported value is `dark`.

### `gui.native_titlebar`

`auto` and `always` keep the native title bar. `never` requests a frameless window on Linux; Windows and macOS retain native controls.

Application theme, text size, and row density are desktop-local preferences managed in the GUI and are not written to this YAML file.

## Interception settings

### `intercept.proxy_addr`

Local HTTP/HTTPS proxy address. Keep it on loopback unless you deliberately intend to expose the proxy.

### `intercept.local_api_addr`

Unauthenticated local API address for `/local/health`, `/local/captures`, and `/local/webhooks`. Keep it on loopback because responses may contain captured data.

### `intercept.shell`

Child shell flavor. Leave empty to infer it from the environment.
