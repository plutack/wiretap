---
title: Capture your first request
description: Start Wiretap interception, send a request, and inspect the captured exchange.
---

This walkthrough keeps traffic on your machine. You do not need to deploy the public relay.

## 1. Open the dashboard

In one terminal, start the desktop GUI:

```sh
wiretap gui
```

Use `wiretap tui` instead when you are working over SSH or installed a CLI-only build.

## 2. Start an intercepted shell

In another terminal:

```sh
wiretap intercept start
```

Wiretap starts a recording proxy on `127.0.0.1:8888` and opens a child shell with proxy and certificate settings scoped to that shell.

:::note
Wiretap does not edit your shell startup files for this workflow. Leaving the child shell restores your previous environment and stops the proxy.
:::

## 3. Make a request

Run this inside the intercepted shell:

```sh
curl https://httpbin.org/anything \
  -H 'Content-Type: application/json' \
  -d '{"hello":"wiretap"}'
```

Open **Traffic** in the dashboard. Select the new row to inspect its request and response headers, body, size, and timing.

![The Traffic tab listing captured exchanges, with each interception session in the sidebar.](/screenshots/03-traffic.png)

The search field filters the complete capture history. Enable **Bodies** inside the
field when the query should also inspect bounded request and response content.
Press `/` to focus search, or open the command palette from the top bar with
`Ctrl K`.

## 4. Continue from the capture

Use **Open in composer** to load the request into an editable draft, or **Export as code** to generate curl, fetch, Python, Go, and other client snippets.

![A capture detail pane showing the request and response headers for one exchange, with the composer and export actions beneath it.](/screenshots/04-capture-detail.png)

## 5. Stop interception

Exit the child shell:

```sh
exit
```

You can also run `wiretap_stop_interception` to restore the environment while leaving that child shell open.

Next, learn [how interception and TLS trust work](/guides/intercept-traffic/) or [add a request transform](/guides/transforms/).
