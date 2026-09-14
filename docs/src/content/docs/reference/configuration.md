---
title: Configuration
description: Reference for the desktop config file, defaults, paths, and GUI-managed preferences.
---

Create the platform-specific configuration file with:

```sh
wiretap config init
```

The config file lives in your platform's user-config directory, with `wiretap` appended:

- **Linux:** `~/.config/wiretap/config.yaml`, or `$XDG_CONFIG_HOME/wiretap/config.yaml` when that variable is set
- **macOS:** `~/Library/Application Support/wiretap/config.yaml`
- **Windows:** `%AppData%\wiretap\config.yaml`

Empty values use platform defaults. Paths may be absolute or relative to the process working directory; prefer absolute paths outside the Wiretap config directory.

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

The WebSocket endpoint used by the desktop tunnel. This is normally `wss://relay.example.com/tunnel`.

:::note
This is the WSS tunnel URL, not the HTTPS base URL accepted by `wiretap relay --url`.
:::

### `relay.forward_url`

When set, every arriving webhook is stored and then replayed to this local URL. Enabled `on_replay` transforms run before forwarding.

### `relay.creds_file`

Registration metadata and the desktop client credential reference. When empty, Wiretap uses `relay-credentials.json` beside the config file. The client token prefers the OS keyring and falls back to this file with mode `0600` on a headless system.

## Local storage

### `store.path`

SQLite database containing captures, delivered webhooks, relay cursors, and transform programs. Protect this file as sensitive data.

## Interface settings

### `tui.theme`

The terminal UI's palette: `dark` or `light`. This defaults to `dark`, and unknown values also fall back to `dark`.

### `gui.native_titlebar`

`auto` and `always` keep the native title bar. `never` requests a frameless window on Linux; Windows and macOS retain native controls.

Application theme, text size, and row density are desktop-local preferences managed in the GUI and are not written to this YAML file.

## Interception settings

### `intercept.proxy_addr`

The address the interception proxy listens on. The intercepted shell sends its `HTTP_PROXY`/`HTTPS_PROXY` traffic here, and the proxy records each request and response it forwards.

:::caution[Keep it on loopback]
The proxy is unauthenticated. Bound to a routable address, anyone who can reach the port can proxy traffic through your machine and request Wiretap CA-signed certificates for arbitrary hosts whenever that CA is trusted. Leave it on `127.0.0.1` unless you deliberately intend that exposure.
:::

### `intercept.local_api_addr`

The address of the read-only control API that runs while `wiretap intercept start` is active. It serves `GET /local/health`, `GET /local/webhooks` (optional `?project=` and `?limit=`), and `GET /local/captures` (optional `?limit=`), newest first.

Keep it on loopback. The API requires no credentials and returns captured request URLs, source IPs, and byte counts — metadata that is sensitive even though response bodies are not included.

### `intercept.shell`

Child shell flavor. Leave empty to infer it from the environment.
