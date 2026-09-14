---
title: Intercept local traffic
description: Control the intercepted shell, understand proxy variables and TLS, and clean up safely.
---

`wiretap intercept start` runs a local HTTP/HTTPS man-in-the-middle proxy and opens a child shell configured to use it. Requests and responses are saved to Wiretap's local store.

## Choose a shell

Wiretap follows `$SHELL` by default. Select a supported shell explicitly when needed:

```sh
wiretap intercept start --shell bash
wiretap intercept start --shell fish
wiretap intercept start --shell powershell
wiretap intercept start --shell gitbash
```

POSIX-compatible shells use the Bash form. Fish and PowerShell receive native syntax.

## Run without a child shell

For CI or another managed process, keep the proxy in the foreground and configure the client yourself:

```sh
wiretap intercept start --no-shell
```

With the default configuration:

```sh
export HTTP_PROXY=http://127.0.0.1:8888
export HTTPS_PROXY=http://127.0.0.1:8888
export SSL_CERT_FILE="$HOME/.local/share/wiretap/ca.crt"
export NODE_EXTRA_CA_CERTS="$HOME/.local/share/wiretap/ca.crt"
```

Press Ctrl-C to stop the proxy.

## TLS trust

Wiretap creates a local certificate authority and issues a certificate for each HTTPS host requested through the proxy. The child shell points compatible clients at that CA without changing your system trust store.

Some tools ignore `SSL_CERT_FILE`, use a bundled trust store, or read a tool-specific setting. Wiretap prepends temporary shims for Git, curl, and Node so their Wiretap-specific CA options stay scoped to the child shell.

To trust the CA system-wide instead:

```sh
sudo wiretap intercept trust-ca
```

System-wide trust affects every process on the machine. Use it only when shell-scoped trust cannot support your client.

:::caution
Anyone who obtains the Wiretap CA private key can impersonate TLS sites to a machine that trusts it. Protect the Wiretap data directory and remove system trust when you no longer need it.
:::

## Stop and clean up

Normally, leave the intercepted child shell with `exit`. To restore its environment without closing it:

```sh
wiretap_stop_interception
```

If a previous version or interrupted setup left managed blocks in shell startup files, remove them with:

```sh
wiretap intercept stop
```

This command removes only Wiretap-managed blocks.

## Use the local control API

While interception is running, the loopback API exposes health and recent records:

```sh
curl http://127.0.0.1:9876/local/health
curl 'http://127.0.0.1:9876/local/captures?limit=50'
curl 'http://127.0.0.1:9876/local/webhooks?project=new-project&limit=50'
```

Keep `intercept.local_api_addr` on loopback. These endpoints are unauthenticated and can return captured data.

## Apply transforms

Enabled `on_request` transforms run before the request goes upstream. Enabled `on_response` transforms run before the response returns to the child process. Changes are captured, so the dashboard shows what the destination or client actually received.

See [Transform payloads](/guides/transforms/) for a complete example.
