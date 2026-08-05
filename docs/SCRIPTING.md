# Payload scripting

wiretap scripts are local JavaScript transformations executed by the pure-Go
[goja](https://github.com/dop251/goja) runtime. They need no Node.js process,
have no filesystem or network API, and run in a fresh runtime for every
exchange. The default timeout is five seconds per script.

Create and manage scripts in the GUI sidebar. A script has a name, trigger,
priority, enabled flag, and JavaScript body. Enabled scripts with the same
trigger run from the lowest priority number to the highest. Each script sees
the mutations made by earlier scripts.

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

### `on_request`

Runs in the interception proxy before an outbound request is sent upstream.
It can modify the method, URL, headers, and body.

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
