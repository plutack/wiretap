# Payload scripting

wiretap scripts are local JavaScript transformations executed by the pure-Go
[goja](https://github.com/dop251/goja) runtime. They need no Node.js process,
have no filesystem or network API, and run in a fresh runtime for every
exchange. The default timeout is five seconds per script.

Create and manage scripts in the GUI sidebar. A script has a name, trigger,
priority, enabled flag, and JavaScript body. Enabled scripts with the same
trigger run from the lowest priority number to the highest. Each script sees
the mutations made by earlier scripts.

## End-to-end walkthrough

This section walks through one full loop: capture real traffic, then rewrite
it with a script, then reject it with a second script. If you have not used
interception yet, read [INTERCEPTION.md](INTERCEPTION.md) first — it explains
what `wiretap intercept start` does to your shell.

### 1. Capture a request

```sh
$ wiretap intercept start
wiretap: interception proxy listening at http://127.0.0.1:8888
...
$ curl -X POST https://api.example.com/orders \
    -H 'Content-Type: application/json' \
    -d '{"item":"coffee","qty":2}'
```

Open `wiretap gui` (any terminal) → **Traffic** tab: the exchange is there
with request/response headers and bodies.

### 2. Rewrite the request with `on_request`

In the GUI sidebar open **Scripts** → create a script:

- **Name:** `tag test runs`
- **Trigger:** `on_request`
- **Priority:** `10`
- **Body:**

```js
// Every outbound JSON body gains a test_run flag and a trace header.
// Requests without a JSON body pass through untouched.
const isJson = (request.headers["Content-Type"] || "").includes("json");
if (!isJson) {
  console.log("skipping non-JSON request to", request.url);
} else {
  const payload = json.parse(request.body || "{}");
  payload.test_run = true;
  request.body = json.stringify(payload);
  request.headers["X-Wiretap-Trace"] = "run-42";
}
```

Save it (it is enabled immediately — no restart needed) and re-run the same
`curl` in the intercepted shell. The Traffic tab now shows the modified body
`{"item":"coffee","qty":2,"test_run":true}` and the new header, and the
upstream server receives the rewritten request.

### 3. Reject a request

Add a second script, trigger `on_request`, priority `20` (after the first):

```js
// Stop the request from ever leaving the machine.
if (request.url.startsWith("https://api.example.com/admin")) {
  reject("admin calls are blocked during test runs");
}
```

`curl` to that URL fails immediately with the rejection reason instead of
reaching the server; the attempted request is still captured. `reject()` also
stops later scripts in the chain — flip the priorities if you want the tag
script to run anyway.

### 4. Transform a delivered webhook with `on_webhook`

For inbound traffic (webhooks delivered over the relay tunnel), scripts run
before the payload is inserted into your local database. A common use is
normalizing noisy payloads:

```js
// Flatten a Stripe-style envelope into just the fields we store.
const event = json.parse(request.body || "{}");
const slim = {
  type: event.type,
  id: event.id,
  created_at: event.created,
};
request.body = json.stringify(slim);
```

Post a webhook to your relay project path (see
[HOSTING.md](HOSTING.md#4-send-and-replay-webhooks)) with the GUI running:
what appears in the Webhooks tab is the slimmed payload. Pair this with the
signature check in [`on_webhook`](#on_webhook) below to drop forgeries.

## Execution model

Scripts receive two mutable globals:

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

Header names are case-insensitive on the Go side, but the JavaScript object is a
normal object. Prefer assigning the canonical spelling you want persisted.
The scripting representation has one string per header name: repeated values
are joined before the script runs. This is convenient for common headers but is
lossy for headers such as `Set-Cookie`. Bodies are exposed as strings, so the
scripting path is intended for text and JSON rather than byte-exact binary
payloads.

Available helpers:

- `reject(reason)` — reject the current exchange and stop the remaining chain.
- `console.log(...)`, `console.error(...)` — captured and shown by a GUI test run;
  pipeline errors are also reported to stderr by the CLI composition roots.
- `crypto.hmac("sha256" | "sha1", key, data)` — lowercase hexadecimal digest.
- `crypto.sha256(data)`, `crypto.sha1(data)` — lowercase hexadecimal digest.
- `base64.encode(text)`, `base64.decode(text)`.
- `json.parse(text)`, `json.stringify(value)`.
- `regex.match(pattern, text)`, `regex.replace(pattern, text, replacement)` —
  Go RE2 syntax, not JavaScript `RegExp` syntax.

A thrown exception or timeout is recorded for that script, but the next script
in the chain still runs. Mutations made before the exception remain visible.
`reject()` is different: it deliberately stops the chain.

## Triggers

Triggers fire at different points in wiretap's pipeline. `on_request` and
`on_response` need interception running (`wiretap intercept start`);
`on_webhook` and `on_replay` work in the GUI/TUI with the relay tunnel
connected.

### `on_request`

Runs in the interception proxy before an outbound request is sent upstream.
It can modify the method, URL, headers, and body.

Redirect every call at one environment to another (a classic "point the CLI
at the staging cluster" move):

```js
// Send api.example.com traffic to the staging host, preserving the path.
const target = "https://staging.example.com";
if (request.url.startsWith("https://api.example.com")) {
  const path = request.url.slice("https://api.example.com".length);
  request.url = target + path;
  request.headers["Host"] = "staging.example.com";
}
```

```js
const payload = json.parse(request.body || "{}");
payload.test_run = true;
request.body = json.stringify(payload);
request.headers["Content-Type"] = "application/json";
request.headers["X-Wiretap-Test"] = "1";
```

### `on_response`

Runs after the upstream response arrives but before it is returned to the
intercepted client. It can modify the status, headers, and body.

```js
if (response.status >= 500) {
  response.status = 200;
  response.headers["Content-Type"] = "application/json";
  response.body = json.stringify({ ok: false, simulated: true });
}
```

This is the current way to alter live responses. Editing a historical capture
cannot change a response that its original client has already received.

### `on_replay`

Runs when the GUI replays a stored webhook to a local target. It is useful for
refreshing timestamps, tokens, or signatures immediately before delivery.

```js
const timestamp = String(Math.floor(Date.now() / 1000));
request.headers["X-Timestamp"] = timestamp;
request.headers["X-Signature"] = crypto.hmac(
  "sha256",
  "replace-with-a-test-secret",
  timestamp + "." + request.body
);
```

Secrets are currently stored as part of the script body in local SQLite. Do not
embed production secrets unless that storage model is acceptable for your
machine.

### `on_webhook`

Runs after a webhook arrives over the relay tunnel but before it is inserted in
the desktop database. It can transform or reject the webhook.

```js
const signature = request.headers["X-Signature"] || "";
const expected = crypto.hmac("sha256", "test-secret", request.body);

if (signature !== expected) {
  reject("signature mismatch");
}
```

A rejected webhook is not inserted locally, but the desktop still acknowledges
it to the relay so it is not delivered forever. Use this only when dropping the
payload is intentional.

## Testing in the GUI

The editor's **Test run** button executes the unsaved editor body against this
built-in sample exchange:

```text
POST https://example.com/webhook
Content-Type: application/json

{"hello":"world"}
```

The test panel shows method, URL, status, request/response bodies, console logs,
rejection state, and script errors. It does **not yet** use the currently
selected capture and does not currently display mutated headers. Save the
script and exercise the appropriate live trigger when header behavior matters.

## Operational notes

- The GUI, TUI, and interception command all load enabled scripts from the local
  SQLite database at execution time; a restart is not required after saving.
- The GUI and TUI start the relay tunnel. `on_webhook` and `on_replay` therefore
  work there. `on_request` and `on_response` require `wiretap intercept start`.
- Lower priority numbers run first. Equal priorities preserve database order;
  use distinct priorities when ordering matters.
- Script errors are non-fatal to the chain. Check the GUI test result or stderr
  before assuming a transformation ran.
- There is no plugin package/import system yet. Scripts are the implemented
  extension mechanism; the planned plugin library remains future work.
