// TrafficDetail renders a captured exchange. NOTE: ListCaptures returns
// summaries only — request/response bodies and full header maps are NOT
// included, and there is no GetCapture binding yet (see PLAN.md §6.2). So this
// pane can only honestly show method/url/status and byte counts. Wire up a
// GetCapture Go binding to enable the full req/resp editor described in §6.2.
import { html } from "../vendor/preact/index.js";
import { MethodBadge, StatusBadge } from "./badges.js";
import { fmtBytes, fmtTime } from "../lib/format.js";

export function TrafficDetail({ capture, onClose }) {
  return html`<aside
    class="flex w-1/2 min-w-[28rem] flex-col overflow-auto border-l border-neutral-800 bg-neutral-900/40"
  >
    <div class="flex items-center gap-2 border-b border-neutral-800 px-4 py-2">
      <h2 class="text-sm font-semibold text-neutral-100">
        Capture #${capture.id}
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
        <${MethodBadge} method=${capture.method} />
        <span class="font-mono text-neutral-300 break-all">${capture.url}</span>
        <${StatusBadge} status=${capture.status} />
      </div>
      <dl class="mb-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
        <dt class="text-neutral-500">At</dt>
        <dd class="font-mono text-neutral-300">${fmtTime(capture.at)}</dd>
        <dt class="text-neutral-500">Request body</dt>
        <dd class="font-mono text-neutral-300">
          ${fmtBytes(capture.req_body_len)}
        </dd>
        <dt class="text-neutral-500">Response body</dt>
        <dd class="font-mono text-neutral-300">
          ${fmtBytes(capture.resp_body_len)}
        </dd>
      </dl>
      <p class="rounded border border-neutral-800 bg-neutral-950 p-2 text-xs text-neutral-500">
        Traffic captures are returned as summaries. The full request/response
        editor (bodies + headers) needs a <code class="font-mono">GetCapture</code>
        binding, which is not wired yet — see PLAN.md §6.2.
      </p>
    </div>
  </aside>`;
}
