// WebhookDetail renders the full webhook (headers, body, replay form). It's a
// controlled component: the parent fetches the webhook and passes it down.
import { html } from "../vendor/preact/index.js";
import { useState } from "../vendor/preact/index.js";
import { MethodBadge, HeaderTable } from "./badges.js";
import { fmtBytes } from "../lib/format.js";

export function WebhookDetail({ webhook, onReplay, onClose }) {
  const [targetURL, setTargetURL] = useState("");
  const [replayState, setReplayState] = useState(null); // {status, error}

  const handleReplay = async () => {
    if (!targetURL.trim()) return;
    setReplayState({ status: "sending" });
    try {
      const result = await onReplay(webhook.project, webhook.seq, targetURL);
      setReplayState({ status: result.status });
    } catch (e) {
      setReplayState({ error: String(e) });
    }
  };

  return html`<aside
    class="flex w-1/2 min-w-[28rem] flex-col overflow-auto border-l border-neutral-800 bg-neutral-900/40"
  >
    <div
      class="flex items-center gap-2 border-b border-neutral-800 px-4 py-2"
    >
      <h2 class="text-sm font-semibold text-neutral-100">
        Webhook ${webhook.project}/${webhook.seq}
      </h2>
      <button
        onClick=${onClose}
        class="ml-auto text-neutral-500 hover:text-neutral-200"
      >
        ✕
      </button>
    </div>
    <div class="flex-1 overflow-auto px-4 py-3 text-sm">
      <div class="mb-3 flex items-center gap-2">
        <${MethodBadge} method=${webhook.method} />
        <span class="font-mono text-neutral-300">${webhook.path}</span>
      </div>

      <section class="mb-4">
        <h3 class="mb-1 text-xs uppercase text-neutral-500">Headers</h3>
        <${HeaderTable} headers=${webhook.headers} />
      </section>

      <section class="mb-4">
        <h3 class="mb-1 text-xs uppercase text-neutral-500">
          Body (${fmtBytes(webhook.body_len)})
        </h3>
        <pre
          class="max-h-48 overflow-auto rounded bg-neutral-950 p-2 text-xs font-mono break-all whitespace-pre-wrap"
          >${webhook.body || "(empty)"}</pre
        >
      </section>

      <section class="mt-4 border-t border-neutral-800 pt-3">
        <h3 class="mb-2 text-xs uppercase text-neutral-500">
          Replay to local target
        </h3>
        <div class="flex gap-2">
          <input
            type="text"
            placeholder="http://127.0.0.1:8080/hook"
            value=${targetURL}
            onInput=${(e) => setTargetURL(e.target.value)}
            class="min-w-0 flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm font-mono"
          />
          <button
            onClick=${handleReplay}
            class="rounded bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500"
          >
            Replay
          </button>
        </div>
        ${replayState &&
        html`<p
          class="mt-2 text-xs ${replayState.error
            ? "text-rose-400"
            : replayState.status === "sending"
              ? "text-neutral-400"
              : "text-emerald-400"}"
        >
          ${replayState.error
            ? `error: ${replayState.error}`
            : replayState.status === "sending"
              ? "sending…"
              : `replayed → HTTP ${replayState.status}`}
        </p>`}
      </section>
    </div>
  </aside>`;
}
