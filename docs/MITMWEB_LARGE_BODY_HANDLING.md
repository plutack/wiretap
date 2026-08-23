# How mitmweb handles large response bodies

> Implementation note (2026-08-23): Wiretap now follows this progressive
> preview model with a 256 KiB initial byte cap and a 100 KiB highlighting cap.
> Unlike mitmweb's current server-side path, Wiretap applies its byte bound in
> the SQLite query before formatting or transferring the preview.

## Scope and conclusion

This report inspects mitmproxy `main` at commit
[`8fc25b3045934777b0796d8ec7958bbc257d7bb5`](https://github.com/mitmproxy/mitmproxy/tree/8fc25b3045934777b0796d8ec7958bbc257d7bb5)
(2026-08-22), and compares current mitmweb with Wiretap's large-response path.

Mitmweb does not make a multi-megabyte response safe by rendering all of it in a
special HTML widget. Its normal viewer instead uses four protections:

1. Flow-list/state JSON omits message bodies and carries only body length and a
   hash ([`flow_to_json`](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/tools/web/app.py#L82-L173)).
2. The selected message's content view is fetched through a separate endpoint
   ([`MessageUtils.getContentURL`](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/flow/utils.ts#L57-L76),
   [`FlowContentView`](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/tools/web/app.py#L690-L755)).
3. Each body request is abortable and component cleanup aborts it when the URL,
   body hash, or mounted selection changes
   ([`useContent`](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/useContent.tsx#L4-L35)).
4. The browser initially receives and mounts at most 513 content-view lines,
   presenting 512 lines plus a progressive **Show more** control
   ([`HttpMessageView`](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/HttpMessage.tsx#L101-L123),
   [`ContentRenderer`](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/ContentRenderer.tsx#L10-L38)).

The closest Wiretap fix is therefore not merely “switch to CodeMirror.” It is to
copy mitmweb's separation of summary metadata from on-demand content, cancellation
of obsolete selections, and a hard initial render budget. Wiretap should improve
on mitmweb by applying a byte budget *before* expensive JSON formatting.

## Data flow in current mitmweb

### The flow list never carries body content

`flow_to_json` explicitly documents that it removes message content to reduce
transmission size. For request and response messages it sends headers, timing,
`contentLength`, and a SHA-256 `contentHash`, but no body
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/tools/web/app.py#L82-L173)).
This same summary representation is used by the `/flows` response
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/tools/web/app.py#L488-L503)).

This is materially different from Wiretap's store/list path: its polling query
selects and scans both full body columns before its GUI summary discards them
([Wiretap store query](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/internal/store/pc.go#L151-L189),
[Wiretap summary conversion](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/internal/gui/bindings.go#L426-L440)).

### Content is fetched on demand and stale fetches are aborted

When the request or response view mounts, `useContentView` constructs a content
URL and delegates loading to `useContent`
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/useContentView.tsx#L29-L63)).
`useContent` creates an `AbortController`, passes its signal to `fetch`, aborts
the preceding controller, and aborts again during effect cleanup
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/useContent.tsx#L4-L35)).
The body hash is an effect dependency, so content changes invalidate the request
even when its URL stays constant
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/useContent.tsx#L33-L35)).

Wiretap instead fetches a complete detail DTO for every click and applies every
completion to global selection without cancellation or an identity guard
([source](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/app.js#L184-L190)).
That permits an obsolete large response to render after the user has moved away.

### The normal viewer has a progressive line budget

The global `content_view_lines_cutoff` defaults to 512 and is described as a
flow-content line limit enabled to speed flow browsing
([constant and option](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/options.py#L8-L8),
[option description](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/options.py#L218-L226)).
The UI requests `maxLines + 1`; clicking **Show more** at least doubles the
allowance, with a floor of 1024
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/HttpMessage.tsx#L108-L123)).
The server accepts `?lines=N` and truncates the content-view text to that number
of lines before serializing the response
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/tools/web/app.py#L690-L755)).

The renderer uses React text children, one `<div>` per returned line, rather
than generating a `<span>` for every JSON token or assigning highlighted
`innerHTML`
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/ContentRenderer.tsx#L10-L38)).
Consequently the default body DOM is bounded to roughly the configured line
count, regardless of how many tokens exist in the original body.

Wiretap's body viewer does the opposite for a 4 MB JSON body: it decodes the full
Base64 value, parses and reserializes all JSON, regex-wraps tokens in spans, then
assigns the complete generated string through `dangerouslySetInnerHTML`
([source](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L45-L65),
[source](https://github.com/plutack/wiretap/blob/f1f6203e3cd86875c37d5b512679a9fa8365a5f9/ui/components/code-block.js#L100-L104)).

## Pretty formatting, highlighting, and CodeMirror

Mitmweb asks the backend content-view registry to choose and execute the pretty
view; the endpoint returns plain `text` plus `view_name`, `syntax_highlight`, and
`description` metadata
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/tools/web/app.py#L690-L716)).
The official content-view documentation says content views return unstyled
strings and may declare a predefined syntax-highlight format
([official docs](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/docs/src/content/addons/contentviews.md#L9-L35)).

In this inspected revision, the read-only HTTP body path uses `ContentRenderer`,
which does not consume the returned `syntax_highlight` field
([viewer call](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/HttpMessage.tsx#L117-L174),
[renderer](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/ContentRenderer.tsx#L10-L38)).
CodeMirror 6 is used only after the user enters edit mode; `HttpMessageEdit`
first fetches raw content and then mounts `CodeEditor`
([edit path](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/HttpMessage.tsx#L45-L86),
[CodeMirror wrapper](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/CodeEditor.tsx#L1-L68)).
There is therefore no CodeMirror viewport virtualization protecting the ordinary
response viewer in this revision; its protection is the server-enforced line
cutoff and incremental **Show more** behavior.

No application-level Web Worker is used in the content-view fetch, formatting,
or render path: those source paths contain fetch/effect logic, a backend
`prettify_message` call, and React rendering, with no worker construction
([fetch path](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/useContent.tsx#L4-L35),
[backend path](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/tools/web/app.py#L690-L755),
[render path](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/ContentRenderer.tsx#L10-L38)).

## Backend size controls are separate from UI preview controls

Mitmproxy buffers complete request and response bodies by default. The official
documentation states that `stream_large_bodies` changes this by forwarding
bodies beyond a configured cutoff without retaining them for inspection or
modification
([official docs](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/docs/src/content/overview/features.md#L355-L378)).
The option accepts byte sizes with `k`, `m`, and `g` suffixes; it defaults to
`None`, so this is an operator-selected capture policy, not an automatic mitmweb
preview limit
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/addons/proxyserver.py#L164-L175)).

`store_streamed_bodies` defaults to false; enabling it retains streamed bodies
at increased memory cost so they remain inspectable
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/addons/proxyserver.py#L149-L157)).
`body_size_limit` is a separate optional byte-size limit, also defaulting to
`None`
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/addons/proxyserver.py#L176-L184)).
When a known or accumulated body size exceeds that limit, the HTTP layer creates
a request/response-too-large protocol error and stops further processing
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/proxy/layers/http/__init__.py#L570-L630)).

These controls protect proxy memory and transfer behavior. They are not the
reason an ordinary retained 4 MB JSON response remains interactive in mitmweb;
that comes primarily from the separate fetch, abort handling, and bounded DOM.

## Important limitation in mitmweb's implementation

The line cutoff is applied *after* `contentviews.prettify_message` returns its
complete text
([source](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/mitmproxy/tools/web/app.py#L690-L706)).
This bounds HTTP response size and browser DOM construction, but it does not
guarantee that the backend content view avoids parsing or formatting the full
body. A pathological formatter may still consume substantial backend CPU and
memory; the browser remains more responsive because that work is not synchronous
JavaScript and DOM construction on its UI thread.

Copy and edit are explicit full-body operations. Copy fetches the same content
view URL without a `lines` parameter, while edit fetches raw `content.data`
without a line limit
([copy path](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/HttpMessage.tsx#L181-L238),
[edit path](https://github.com/mitmproxy/mitmproxy/blob/8fc25b3045934777b0796d8ec7958bbc257d7bb5/web/src/js/components/contentviews/HttpMessage.tsx#L45-L86)).
Those opt-in paths can still be expensive for very large bodies.

## Concrete lessons for Wiretap

| Concern | Current mitmweb | Current Wiretap | Recommended Wiretap behavior |
| --- | --- | --- | --- |
| List/poll payload | Metadata, length, hash; body omitted | SQL loads bodies, summary later discards them | Use a summary-only query with `length(...)` |
| Detail loading | Separate content endpoint on view mount | Full detail DTO on every row click | Fetch metadata and a bounded body preview separately |
| Stale navigation | `AbortController` cancels cleanup/URL changes | Every completion calls `setSelection` | Cancel plus latest-request-wins identity check |
| Default render budget | 512 lines, fetched as 513 to detect overflow | Complete body below 15 MiB | Limit by bytes and lines before decode/format/render |
| DOM shape | One text `<div>` per preview line | One highlighted `<span>` per JSON token | Plain text for large previews; highlight only small bodies |
| Full content | Explicit Copy/Edit/raw-content operation | Automatically transported and processed | Make Save/Copy/full inspection explicit |
| Formatting location | Backend content view, then line cutoff | Browser UI thread, complete body | Prefer bounded backend preview; worker only as secondary defense |

For the reported 4,296,720-byte JSON, the highest-value design is:

1. Keep list polling body-free.
2. On selection, request metadata plus at most a byte-bounded preview (for
   example 256 KiB), carrying a `truncated` flag and original byte length.
3. Abort or ignore the result when another row is selected or the pane closes.
4. Render the preview as text with a line/DOM cap. Only format and highlight
   below a much smaller threshold.
5. Provide Save/Copy/full-body actions as deliberate operations.

Unlike mitmweb's current post-format line cutoff, applying Wiretap's byte cutoff
before Base64 decoding, JSON parsing, and highlighting also eliminates its
largest synchronous computation and temporary-string expansions.
