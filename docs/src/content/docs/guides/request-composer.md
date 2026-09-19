---
title: Compose and replay requests
description: Author requests, import JSON, continue from a capture, or prepare a draft with a source recipe.
---

Open **Compose** in the desktop GUI to send an ad-hoc HTTP request. The request is not added to capture history automatically.

## Write a request manually

Choose a method, enter an absolute `http://` or `https://` URL, edit headers, and add a body. Header values may be strings or arrays.

![The Compose workbench with an empty request draft, offering manual entry, a source recipe, file import, and body examples.](/screenshots/05-compose.png)

Enable **Apply on_replay transforms** when the request should pass through the same enabled transform chain used by webhook replay. A transform can change or reject the request, but the final destination must still be an absolute HTTP(S) URL.

Requests have a 30-second timeout and use a direct local transport rather than the interception proxy.

## Import JSON

Importing an ordinary JSON object or array creates a `POST` request body with `Content-Type: application/json`.

To set the complete request, import an envelope with a top-level `url`:

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

Defaults are `POST`, JSON content type, and `apply_transforms: true`. Non-string bodies are JSON-encoded and formatted.

## Continue from captured data

Select **Open in composer** from a webhook or traffic detail pane. For an intercepted request, Wiretap loads the complete request body on demand instead of copying only the bounded preview.

The resulting draft is editable and is not sent until you choose **Send**.

## Prepare a request with a source recipe

Use **Source recipe** when the input is a provider log, nested event envelope, or another shape that is not already an HTTP request.

1. Create and enable a transform with the `on_compose` trigger.
2. Paste or import the source record.
3. Enter a target base URL.
4. Select the recipe and choose **Transform request**.
5. Review the generated request in **Manual request**, then send it.

Recipe application performs no network I/O. It also leaves `on_replay` transforms disabled for the generated draft until you explicitly enable them.

Example recipe:

```js
const source = json.parse(request.body);
request.method = "POST";
request.url = request.url.replace(/\/+$/, "") + "/webhook/" + source.path;
request.headers["Content-Type"] = "application/json";
request.body = json.stringify(source.payload);
```

## Response limits

The composer shows status, duration, response headers, content type, body size, and truncation state. It retains at most 2 MiB of a response in memory and across the desktop bridge. A larger response is clearly marked as truncated.
