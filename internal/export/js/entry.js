// Bundle entry for the embedded httpsnippet engine (see ../export.go).
//
// httpsnippet-lite converts a HAR request object into a ready-to-run code
// snippet (curl, fetch, python-requests, go, …). This file is the esbuild
// entry that wraps it behind two stable globals the Go side calls through
// goja:
//
//   wiretapSnippet(harJson, target, client) -> Promise<string>
//   wiretapTargets()                        -> string (JSON array)
//
// Rebuild the committed bundle with `make snippet-bundle` (or
// `npm run snippet-bundle`) after changing this file or bumping the
// httpsnippet-lite dependency. The bundle targets ES2017 because goja
// implements ES6+ but not the newest syntax; esbuild lowers everything
// else (async/await stays, which goja supports).
import { HTTPSnippet, availableTargets } from "httpsnippet-lite";

// wiretapSnippet renders one HAR request as a code snippet. `client` may be
// an empty string to use the target's default client. The promise resolves
// synchronously in practice: esbuild rewrites httpsnippet-lite's dynamic
// imports into plain requires, so the Go caller can read the settled value
// as soon as goja drains its job queue.
globalThis.wiretapSnippet = function (harJson, target, client) {
  const snippet = new HTTPSnippet(JSON.parse(harJson));
  return snippet.convert(target, client || undefined).then(function (out) {
    // convert() returns null for an unknown target (an unknown client throws
    // on its own) and string | string[] otherwise; we always pass a single
    // request, so unwrap the array form.
    if (out === null || out === undefined) {
      throw new Error("unknown target: " + target);
    }
    return Array.isArray(out) ? out.join("\n") : String(out);
  });
};

// wiretapTargets returns the target catalog as JSON:
//   [{key, title, default, clients: [{key, title}]}, …]
globalThis.wiretapTargets = function () {
  const list = availableTargets().map(function (t) {
    return {
      key: t.key,
      title: t.title,
      default: t.default,
      clients: t.clients.map(function (c) {
        return { key: c.key, title: c.title };
      }),
    };
  });
  return JSON.stringify(list);
};
