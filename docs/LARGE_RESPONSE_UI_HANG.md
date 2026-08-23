# Large response body UI hang

## Implementation status

Implemented on 2026-08-23:

- capture-list polling now selects metadata and `length(body)` values without
  loading body blobs;
- opening a capture returns at most 256 KiB from each body, Base64-encoded once,
  together with the complete length and a truncation flag;
- the viewer offers progressive, doubling “Show more” requests and retrieves
  the complete body only for explicit Save or Copy All actions;
- JSON formatting and token-span highlighting are limited to complete bodies
  no larger than 100 KiB; larger previews render as one plain-text node;
- capture selection is latest-request-wins, and close/tab changes invalidate
  pending selections.

The initial row-open path is therefore bounded independently of the stored body
size. Choosing Show more, Save, or Copy All can still allocate larger buffers,
but those costs are explicit and do not generate the former token-per-span DOM.

## Scope and conclusion

The reported payload from `https://models.opencode.ai/api.json` was fetched on
2026-08-23. Its decoded response is 4,296,720 bytes, has
`Content-Type: application/json`, and is a JSON object. The most likely primary
cause of the hang is not network latency: opening the row synchronously expands
that body several times and constructs a very large highlighted DOM on the UI
thread. Rapid switching repeats the work and also exposes an uncancelled,
last-completion-wins selection race.

Confidence is **high** in the hot-path diagnosis based on direct code tracing
and a diagnostic run against the exact payload. A WebKitGTK performance profile
would still be useful to divide elapsed time between JavaScript, DOM parsing,
layout, and garbage collection.

## Selection-to-render data flow

1. A traffic-row click invokes `onSelect(capture.id)`
   ([`ui/components/traffic-list.js:51-55`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/traffic-list.js#L51-L55)).
2. `openCapture` awaits the full capture and then replaces the global selection
   ([`ui/app.js:184-190`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/app.js#L184-L190)).
3. The detail pane passes both response text and response Base64 into
   `BodyViewer`
   ([`ui/components/traffic-detail.js:61-70`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/traffic-detail.js#L61-L70)).
4. `BodyViewer` decodes, formats, highlights, and injects the complete result as
   `innerHTML`
   ([`ui/components/code-block.js:45-65`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L45-L65),
   [`ui/components/code-block.js:100-104`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L100-L104)).

## Evidence

### 1. The 4 MiB body takes the complete synchronous preview path

The cutoff is 15 MiB, so the reported 4,296,720-byte response is treated as
safe to preview
([`ui/components/code-block.js:9,52`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L9-L52)).
For JSON, one render then performs all of the following:

- `atob`, followed by a JavaScript byte-by-byte copy into a `Uint8Array`
  ([`ui/components/code-block.js:22-27`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L22-L27));
- a full UTF-8 decode
  ([`ui/components/code-block.js:30-31,49-51`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L30-L51));
- `JSON.parse`, followed by an indented `JSON.stringify`
  ([`ui/lib/format.js:68-79`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/lib/format.js#L68-L79));
- whole-string HTML escaping and regex tokenization, producing a `<span>` per
  matched token
  ([`ui/lib/format.js:87-117`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/lib/format.js#L87-L117));
- parsing and mounting that generated markup with `dangerouslySetInnerHTML`.

A local Node diagnostic calling the repository's actual `prettyBody` and
`highlightJSON` functions on the fetched payload measured:

| Stage/output | Result |
| --- | ---: |
| Downloaded JSON | 4,296,720 bytes |
| Pretty-printed text | 7,397,016 characters |
| Highlighted HTML | 17,798,768 characters |
| Generated `<span>` elements | 357,268 |
| `prettyBody` elapsed | 104.2 ms |
| `highlightJSON` elapsed | 471.2 ms |
| Node heap after the run | 245.1 MiB |

These timings are diagnostic, not a WebKitGTK benchmark. They exclude Base64
transport/decode and DOM creation, so they understate the application's total
cost. The element count and output expansion explain why the UI can remain
unresponsive substantially longer than the pure string operations. JavaScript
normally runs on the page's main thread; browser guidance recommends workers
for expensive computation so it does not block UI interaction
([MDN: Using Web Workers](https://developer.mozilla.org/en-US/docs/Web/API/Web_Workers_API/Using_web_workers)).

The warning does not protect larger bodies from CPU/memory expansion either.
`isLarge` is checked only in the returned template, after Base64 decode, text
decode, JSON formatting, highlighting (or hex generation), and creation of
`inner` have already happened
([`ui/components/code-block.js:49-65,100-104`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L49-L104)).

### 2. The detail payload duplicates every body

The Go DTO carries each body as both a string and Base64
([`internal/gui/bindings.go:77-93`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/internal/gui/bindings.go#L77-L93)),
and `captureDetail` eagerly creates both forms
([`internal/gui/bindings.go:443-461`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/internal/gui/bindings.go#L443-L461)).
For this 4,296,720-byte body, Base64 alone is 5,728,960 characters; before the
viewer starts work, the bridge payload therefore includes roughly 10 million
characters across the two body fields, plus JSON framing. The viewer always
prefers the Base64 field when present, making the duplicate text unused for
rendering
([`ui/components/code-block.js:49`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L49)).

`atob()` itself returns a binary string, after which this code allocates another
byte array and copies every character. That API behavior is documented by the
browser platform owner documentation
([MDN: `Window.atob()`](https://developer.mozilla.org/en-US/docs/Web/API/Window/atob)).

### 3. Switching and polling repeat expensive work

Only the Base64-to-bytes and bytes-to-text stages use `useMemo`. JSON
parse/stringify, escaping, highlighting, and hex generation occur directly in
the component body on every render
([`ui/components/code-block.js:49-65`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L49-L65)).
The app updates capture, webhook, and session state every two seconds
([`ui/app.js:130-142`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/app.js#L130-L142)),
rerendering the un-memoized detail subtree even when its selected capture has
not changed. Preact describes `useMemo` as a way to cache expensive
computations between renders
([Preact Hooks: `useMemo`](https://preactjs.com/guide/v10/hooks/#usememo)).

Closing and reopening remounts the viewer, so even its two memoized stages run
again. Rapid switching therefore creates repeated bursts of large temporary
strings and DOM nodes, increasing garbage-collection pressure.

Git history narrows the current amplification to
[`00e338d`, “add content-aware body viewer”](https://github.com/plutack/wiretap/commit/00e338d5f1eb314c8780998982a6f413e613e2b5):
that change introduced the 15 MiB cutoff, Base64 decode loop, content-aware
formatting, and large highlighted `innerHTML`. The earlier viewer memoized
`prettyBody`; the current one no longer does.

### 4. Rapid selection has a stale-result race

Each click starts an asynchronous `GetCapture`, but `openCapture` has no request
identity check or cleanup
([`ui/app.js:184-190`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/app.js#L184-L190)).
The generated binding labels the result a `CancellablePromise`
([`ui/bindings/.../bindings.js:64-73`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/bindings/github.com/plutack/wiretap/internal/gui/bindings.js#L64-L73)),
but neither the API wrapper nor the selection handler retains/cancels it. Thus,
overlapping clicks are last-*completion*-wins rather than last-click-wins. A
large stale response can replace a newer selection; a response completing after
`closeDetail` can reopen the pane because close only sets selection to null
([`ui/app.js:250`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/app.js#L250)).

This race is not by itself the long synchronous pause, but it makes rapid
switching remount and process bodies the user has already navigated away from.

### 5. Background polling also reloads discarded bodies from SQLite

The GUI list contract says bodies are omitted
([`internal/gui/bindings.go:200-208`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/internal/gui/bindings.go#L200-L208)),
but the store query selects and scans both full bodies for as many as 100 rows
([`internal/store/pc.go:151-189`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/internal/store/pc.go#L151-L189)).
`captureSummary` then discards those bytes and keeps only their lengths
([`internal/gui/bindings.go:426-440`](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/internal/gui/bindings.go#L426-L440)).
Because capture polling runs every two seconds, a large row adds recurring DB
I/O and Go allocation even while merely switching views. This is secondary to
the main-thread preview cost, but it can worsen responsiveness and memory churn.

## Recommended fix order

1. **P0 — Make the size decision before body materialization and formatting.**
   Do not automatically preview multi-megabyte text. Render metadata and an
   explicit “Preview anyway”/“Save” choice using `bodyLength` before `atob`,
   `TextDecoder`, JSON parsing, highlighting, or hex generation. Independently
   cap the number of previewed bytes and DOM tokens; CSS `max-height` limits the
   viewport, not the amount of DOM.
2. **P0 — Make row selection latest-request-wins.** Track a monotonically
   increasing selection request ID (including close/tab changes) and ignore
   stale completions. Cancel the underlying binding call as well if the Wails
   runtime contract used by this build preserves cancellation through the
   wrapper.
3. **P1 — Stop duplicating body representations.** Return a tagged text-or-Base64
   representation, or fetch/download raw body bytes only on demand. A text JSON
   body should not cross the bridge both verbatim and Base64-encoded.
4. **P1 — Bound and defer rendering.** Memoize derived preview output by body and
   mode. For previews still large enough to be noticeable, move JSON formatting
   off the UI thread with a worker and render a truncated or virtualized text
   view. A worker improves responsiveness but does not make a 357,000-node DOM
   safe, so the DOM cap remains necessary.
5. **P2 — Add a summary-specific SQL query.** Select metadata plus
   `length(req_body)`/`length(resp_body)` rather than loading bodies that the
   summary DTO immediately discards.

## Verification plan for an implementation

- Add a unit test proving a body over the preview limit never calls Base64
  decode, JSON formatting, highlighting, or hex generation until explicit user
  opt-in.
- Add a selection-race test: start slow capture A, select capture B, complete B,
  then complete A; B must remain selected. Repeat with close after starting A;
  the pane must stay closed.
- Use the exact 4,296,720-byte fixture in a browser-level test and assert the
  close/row-switch interaction is processed promptly. Record WebKitGTK main
  thread and heap profiles before and after the fix.
- Verify polling SQL does not return body columns, while list rows retain exact
  body lengths.

## Reproduction metadata

- Repository revision inspected:
  `f1f6203e3cd86875c37d5b512679a9fa8365a5f9`.
- Endpoint fetched: `https://models.opencode.ai/api.json`.
- Response observed: HTTP 200, `Content-Type: application/json`, Brotli content
  encoding, 4,296,720 decoded bytes.
- Diagnostic command executed the repository's exported `prettyBody` and
  `highlightJSON` functions under Node. No application code was changed.
