# Request composer

The desktop GUI's **Compose** workspace sends ad-hoc HTTP requests without
requiring a previously captured webhook. It is intended for exercising local
development endpoints and replaying representative JSON payloads.

## Creating a request

Choose a method, enter an absolute HTTP or HTTPS URL, edit the headers JSON,
and enter the request body. Header values can be strings or arrays; they are
normalized to HTTP's multi-value header representation before sending.

Enable **Apply on_replay transforms** to run the same enabled transform chain
used by stored webhook replay. A transform may change the method, URL, headers,
or body, or reject the request. Both the original and transformed destination
must be absolute HTTP(S) URLs.

Requests use a 30-second timeout and bypass the interception proxy, matching
the existing local webhook replay behavior.

## Importing JSON

Use **Import JSON** to select a `.json` file. A normal JSON object or array is
pretty-printed into the body and defaults to:

```text
POST
Content-Type: application/json
```

If the top-level object has a `url` field, it is treated as a request envelope:

```json
{
  "method": "PATCH",
  "url": "http://127.0.0.1:8080/orders/123",
  "headers": {
    "Content-Type": "application/json",
    "Authorization": ["Bearer local-development-token"]
  },
  "body": {
    "status": "paid"
  },
  "apply_transforms": false
}
```

`method` defaults to `POST`, `headers` defaults to JSON content type, and
`apply_transforms` defaults to true. A non-string `body` is JSON-encoded and
pretty-printed. JSON can also be pasted directly into the body editor.

## Continue from captured data

Webhook and traffic detail panes include **Open in composer**. For traffic
captures, the complete request body is loaded on demand rather than using the
bounded list/detail preview. The resulting draft remains editable before it is
sent.

## Responses and limits

The response panel shows:

- HTTP status;
- elapsed request time;
- response headers;
- detected content type and formatted body;
- response body size and truncation state.

Wiretap retains at most 2 MiB of a composed response in memory and across the
GUI bridge. If the endpoint sends a larger response, the composer clearly
marks the preview as truncated. Composed requests and responses are not stored
in the capture database automatically.
