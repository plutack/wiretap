# Request composer

The desktop GUI's **Compose** workspace sends ad-hoc HTTP requests without
requiring a previously captured webhook. It is intended for exercising local
development endpoints and replaying representative JSON payloads.

The workspace has two input modes. **Manual request** is the original composer.
**Source recipe** converts an arbitrary log or provider envelope into a complete
editable request before anything is sent.

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

## Preparing a request with a recipe

A compose recipe is a normal local JavaScript transform with the `on_compose`
trigger. Create it from the **Transforms** section, save and enable it, then
choose it under **Compose > Source recipe**.

Paste source text or import a JSON file, enter the target base URL, and select
**Transform request**. The source is exposed as `request.body`; the target base
URL is exposed as `request.url`. The recipe can replace the method, URL,
headers, and body using the standard scripting API.

Recipe execution is a preparation step only. Wiretap returns to the manual
composer with the generated request visible and editable. It does not send a
network request, and it leaves **Apply on_replay transforms** disabled on the
generated draft unless the user turns it back on.

Wiretap includes no provider-specific recipes in the application. Recipes are
user-owned local scripts. A copyable example for the Nuvion Heroku Bridge/Fuse
workflow is available at
[`docs/recipes/nuvion-heroku-webhook.js`](recipes/nuvion-heroku-webhook.js).

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
