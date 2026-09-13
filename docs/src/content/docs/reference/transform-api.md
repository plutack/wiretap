---
title: Transform API
description: Globals, helpers, triggers, execution order, errors, and sandbox limits for Wiretap JavaScript transforms.
---

Wiretap executes transforms with the pure-Go goja JavaScript runtime. Every script receives mutable `request` and `response` globals.

## Exchange globals

```js
request = {
  method: "POST",
  url: "https://example.com/path",
  headers: { "Content-Type": "application/json" },
  body: '{"hello":"world"}'
};

response = {
  status: 200,
  headers: { "Content-Type": "application/json" },
  body: '{"ok":true}'
};
```

Header names are case-insensitive after returning to Go, but the JavaScript object is an ordinary object. Assign the canonical spelling you want stored.

Repeated header values are joined into one string before a script runs. This representation is convenient for common headers but lossy for headers such as `Set-Cookie`. Bodies are strings, so transforms are intended for text and JSON rather than byte-exact binary content.

## Helpers

### Control and logging

- `reject(reason)` rejects the exchange and stops the remaining chain.
- `console.log(...)` and `console.error(...)` appear in GUI test results. Pipeline errors are also reported by CLI composition roots.

### Crypto

- `crypto.hmac("sha256" | "sha1", key, data)` returns a lowercase hex digest.
- `crypto.sha256(data)` returns a lowercase hex digest.
- `crypto.sha1(data)` returns a lowercase hex digest.

### Data

- `base64.encode(text)` and `base64.decode(text)`.
- `json.parse(text)` and `json.stringify(value)`.
- `regex.match(pattern, text)`.
- `regex.replace(pattern, text, replacement)`.

Regex helpers use Go RE2 syntax, not JavaScript `RegExp` syntax.

## Triggers

### `on_request`

Runs before an intercepted request is sent upstream. It can modify method, URL, headers, and body.

```js
if (request.url.startsWith("https://api.example.com")) {
  request.url = request.url.replace(
    "https://api.example.com",
    "https://staging.example.com"
  );
  request.headers["Host"] = "staging.example.com";
}
```

### `on_response`

Runs after the upstream response arrives but before it reaches the intercepted client.

```js
if (response.status >= 500) {
  response.status = 200;
  response.headers["Content-Type"] = "application/json";
  response.body = json.stringify({ ok: false, simulated: true });
}
```

### `on_replay`

Runs before stored webhook replay and before a composed request when the user opts in.

```js
const timestamp = String(Math.floor(Date.now() / 1000));
request.headers["X-Timestamp"] = timestamp;
request.headers["X-Signature"] = crypto.hmac(
  "sha256",
  "replace-with-a-test-secret",
  timestamp + "." + request.body
);
```

Transform programs are stored in local SQLite. Do not embed production secrets unless that storage model is acceptable.

### `on_webhook`

Runs after a webhook arrives over the relay tunnel but before local insertion.

```js
const signature = request.headers["X-Signature"] || "";
const expected = crypto.hmac("sha256", "test-secret", request.body);

if (signature !== expected) {
  reject("signature mismatch");
}
```

A rejected webhook is not stored locally, but the desktop acknowledges it so the relay does not deliver it forever.

### `on_compose`

Runs only when explicitly selected as a Compose source recipe. `request.body` contains the raw source text and `request.url` contains the entered target base URL. Applying the recipe produces an editable draft and does not send it.

## Ordering and failure

Enabled transforms run from lowest priority number to highest. Each sees mutations made by earlier scripts.

The default timeout is five seconds per script. A thrown exception or timeout is recorded and later scripts continue. Mutations made before the failure remain. `reject()` deliberately stops the chain.

Each execution uses a fresh runtime with no filesystem or network API.
