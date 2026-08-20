// CodeBlock renders a request/response body. When the payload is JSON it is
// pretty-printed (2-space indent) and syntax-highlighted; otherwise the raw
// text is shown verbatim. A small toolbar lets the user copy the body and, for
// JSON, toggle between the formatted and raw views. This is what replaces the
// old unformatted `<pre>` blobs that showed JSON "all over the place".
import { html } from "../vendor/preact/index.js";
import { useMemo, useState } from "../vendor/preact/index.js";
import { prettyBody, highlightJSON, escapeHTML } from "../lib/format.js";
import { copyText } from "../lib/clipboard.js";

export function CodeBlock({ body, contentType, maxHeightClass = "max-h-72" }) {
  const [raw, setRaw] = useState(false);
  const [copyState, setCopyState] = useState("idle");

  const { text, isJSON } = useMemo(
    () => prettyBody(body, contentType),
    [body, contentType],
  );

  if (!body) {
    return html`<p class="rounded-md border border-dashed border-neutral-800 px-3 py-2 text-xs text-neutral-600">
      (empty)
    </p>`;
  }

  const showFormatted = isJSON && !raw;
  const rendered = showFormatted ? text : body;
  const inner = showFormatted ? highlightJSON(text) : escapeHTML(rendered);

  const copy = async () => {
    try {
      await copyText(rendered);
      setCopyState("copied");
    } catch (error) {
      console.error("copy body:", error);
      setCopyState("failed");
    }
    setTimeout(() => setCopyState("idle"), 1600);
  };

  return html`<div class="overflow-hidden rounded-md border border-neutral-800 bg-neutral-950">
    <div class="flex items-center gap-2 border-b border-neutral-800 bg-neutral-900/60 px-2 py-1">
      ${isJSON
        ? html`<span class="chip bg-brand-500/15 text-brand-300">json</span>`
        : html`<span class="chip bg-neutral-700/40 text-neutral-400">text</span>`}
      <div class="ml-auto flex items-center gap-1">
        ${isJSON
          ? html`<button
              onClick=${() => setRaw((r) => !r)}
              class="rounded px-1.5 py-0.5 text-xs text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200"
            >
              ${raw ? "Formatted" : "Raw"}
            </button>`
          : null}
        <button
          onClick=${copy}
          class="rounded px-1.5 py-0.5 text-xs text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200"
        >
          ${copyState === "copied" ? "Copied" : copyState === "failed" ? "Copy failed" : "Copy"}
        </button>
      </div>
    </div>
    <pre
      class="${maxHeightClass} overflow-auto p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words"
      dangerouslySetInnerHTML=${{ __html: inner }}
    ></pre>
  </div>`;
}
