// ExportSnippet renders the "Export as code" section shown in both detail
// panes (traffic captures and webhooks). It loads the httpsnippet target
// catalog once per session, lets the user pick a language + client, converts
// the selected exchange through the Go bindings (internal/export), and shows
// the result in a monospace block with a copy button.
//
// The component is detail-pane-agnostic: the parent passes `convert`, an
// async (target, client) => string callback bound to the selected row, plus
// `exportKey` — a stable identity for the row so switching selection resets
// the rendered snippet but keeps the chosen language.
import { html } from "../vendor/preact/index.js";
import { useEffect, useMemo, useState } from "../vendor/preact/index.js";
import { api } from "../lib/api.js";
import { copyText } from "../lib/clipboard.js";
import { Select } from "./ui.js";
import { escapeHTML } from "../lib/format.js";

// Module-level cache: the catalog is static for the process lifetime.
let targetsPromise = null;
function loadTargets() {
  if (!targetsPromise) targetsPromise = api.exportTargets();
  return targetsPromise;
}

// Sticky selection across rows/panes — picking "python/requests" once should
// survive opening the next capture.
const sticky = { target: "shell", client: "" };

export function ExportSnippet({ exportKey, convert }) {
  const [targets, setTargets] = useState([]);
  const [target, setTarget] = useState(sticky.target);
  const [client, setClient] = useState(sticky.client);
  const [snippet, setSnippet] = useState("");
  const [error, setError] = useState("");
  const [copyState, setCopyState] = useState("idle");

  useEffect(() => {
    let alive = true;
    loadTargets().then(
      (ts) => alive && setTargets(ts || []),
      (e) => alive && setError("load targets: " + e),
    );
    return () => {
      alive = false;
    };
  }, []);

  const active = useMemo(
    () => targets.find((t) => t.key === target) || null,
    [targets, target],
  );

  // Re-convert whenever the row or the language/client selection changes —
  // debounced, because conversion spins a JS runtime on the Go side per
  // call. Clicking rapidly through capture rows otherwise fires one
  // conversion per click, and the pile-up froze the UI.
  useEffect(() => {
    if (!active) return undefined;
    let alive = true;
    setError("");
    const timer = setTimeout(() => {
      convert(target, client).then(
        (out) => alive && setSnippet(out),
        (e) => {
          if (!alive) return;
          setSnippet("");
          setError(String(e));
        },
      );
    }, 250);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [exportKey, target, client, active]);

  const pickTarget = (key) => {
    sticky.target = key;
    sticky.client = "";
    setTarget(key);
    setClient("");
  };
  const pickClient = (key) => {
    sticky.client = key;
    setClient(key);
  };

  const copy = async () => {
    try {
      await copyText(snippet);
      setCopyState("copied");
    } catch (e) {
      console.error("copy snippet:", e);
      setCopyState("failed");
    }
    setTimeout(() => setCopyState("idle"), 1600);
  };

  const clientOptions = active
    ? [
        { value: "", label: `default (${active.default})` },
        ...active.clients.map((c) => ({ value: c.key, label: c.title })),
      ]
    : [{ value: "", label: "default" }];

  // Stable object identity so the snippet <pre> is not re-assigned
  // innerHTML on unrelated re-renders (the poll loop).
  const snippetHTML = useMemo(() => ({ __html: escapeHTML(snippet) }), [snippet]);

  return html`<section class="inspector-section border-t border-neutral-800 pt-4">
    <div class="inspector-label">Export as code</div>
    <div class="flex gap-2">
      <${Select}
        aria-label="Snippet language"
        value=${target}
        onChange=${(e) => pickTarget(e.target.value)}
        options=${targets.map((t) => ({ value: t.key, label: t.title }))}
      />
      <${Select}
        aria-label="Snippet client"
        value=${client}
        onChange=${(e) => pickClient(e.target.value)}
        options=${clientOptions}
      />
    </div>
    ${error
      ? html`<p class="mt-2 text-xs text-rose-400">${error}</p>`
      : html`<div class="mt-2 overflow-hidden rounded-md border border-neutral-800 bg-neutral-950">
          <div class="flex items-center gap-2 border-b border-neutral-800 bg-neutral-900/60 px-2 py-1">
            <span class="chip bg-brand-500/15 text-brand-300">${target}${client ? "/" + client : ""}</span>
            <div class="ml-auto">
              <button
                onClick=${copy}
                class="rounded px-1.5 py-0.5 text-xs text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200"
              >
                ${copyState === "copied" ? "Copied" : copyState === "failed" ? "Copy failed" : "Copy"}
              </button>
            </div>
          </div>
          <pre
            class="max-h-72 overflow-auto p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words"
            dangerouslySetInnerHTML=${snippetHTML}
          ></pre>
        </div>`}
  </section>`;
}
