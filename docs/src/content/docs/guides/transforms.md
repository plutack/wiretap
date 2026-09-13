---
title: Transform payloads
description: Create, test, order, import, and export local JavaScript transforms.
---

Transforms are local JavaScript programs. They run without Node.js in a fresh sandbox for each exchange and have no filesystem or network API.

## Rewrite an intercepted request

Open **Transforms** in the GUI and create:

- **Name:** `tag test runs`
- **Trigger:** `on_request`
- **Priority:** `10`

Use this program:

```js
const contentType = request.headers["Content-Type"] || "";

if (contentType.includes("json")) {
  const payload = json.parse(request.body || "{}");
  payload.test_run = true;
  request.body = json.stringify(payload);
  request.headers["X-Wiretap-Trace"] = "run-42";
} else {
  console.log("skipping non-JSON request to", request.url);
}
```

Save the transform, start an intercepted shell, and send a JSON request. The destination and Traffic view receive the changed body and header.

## Reject a request

Add another `on_request` transform at priority `20`:

```js
if (request.url.startsWith("https://api.example.com/admin")) {
  reject("admin calls are blocked during test runs");
}
```

`reject()` stops the request and the remaining transform chain. A thrown exception is different: it is recorded for that script, but later scripts continue and earlier mutations remain.

## Choose the right trigger

| Trigger | When it runs | Typical use |
| --- | --- | --- |
| `on_request` | Before intercepted traffic goes upstream | Redirect, add headers, reject |
| `on_response` | Before an intercepted response reaches its client | Simulate a status or body |
| `on_webhook` | Before a delivered webhook is stored locally | Verify or normalize inbound payloads |
| `on_replay` | Before webhook replay or an opted-in composed request | Refresh timestamps or signatures |
| `on_compose` | Only when selected as a Compose source recipe | Turn a source record into a request draft |

Enabled transforms with the same trigger run from lowest priority number to highest. Use distinct priorities when order matters.

## Test before saving

The transform editor's test bench accepts a sample method, URL, headers, request body, response status, and response body. It runs the unsaved program and shows the transformed exchange, console output, rejection, and errors without sending network traffic.

## Import, export, and duplicate

**Export file** writes the current editor state as a portable JSON document. **Import** validates a document and opens it as an unsaved draft. **Duplicate** creates a disabled, unsaved copy of the current transform.

```json
{
  "format": "wiretap-transform",
  "version": 1,
  "name": "prepare order webhook",
  "trigger": "on_compose",
  "priority": 0,
  "enabled": true,
  "program": "request.method = \"POST\";"
}
```

Imports are limited to 1 MiB and reject unknown fields, unsupported versions, missing names, and invalid triggers. IDs and timestamps are intentionally excluded.

For every global and helper, see the [Transform API](/reference/transform-api/).
