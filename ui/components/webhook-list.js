// WebhookList renders a table of webhook summaries (project, seq, method, path,
// received time, body length). Clicking a row calls onSelect with (project, seq).
import { html } from "../vendor/preact/index.js";
import { MethodBadge } from "./badges.js";
import { EmptyState } from "./ui.js";
import { fmtTime, fmtBytes } from "../lib/format.js";

export function WebhookList({ webhooks, onSelect, selectedKey }) {
  if (webhooks.length === 0) {
    return html`<${EmptyState}
      icon="⛁"
      title="No webhooks captured yet"
      hint="Webhooks forwarded through your relay will appear here."
    />`;
  }

  return html`<table class="w-full border-collapse text-sm">
    <thead class="sticky top-0 z-10 bg-neutral-900 text-left">
      <tr class="text-[11px] font-semibold tracking-wider text-neutral-500 uppercase">
        <th class="px-3 py-2 font-semibold">Project</th>
        <th class="px-3 py-2 font-semibold">Seq</th>
        <th class="px-3 py-2 font-semibold">Method</th>
        <th class="px-3 py-2 font-semibold">Path</th>
        <th class="px-3 py-2 font-semibold">Received</th>
        <th class="px-3 py-2 text-right font-semibold">Body</th>
      </tr>
    </thead>
    <tbody>
      ${webhooks.map((w) => {
        const key = `${w.project}-${w.seq}`;
        const active = key === selectedKey;
        return html`<tr
          key=${key}
          onClick=${() => onSelect(w.project, w.seq)}
          class="cursor-pointer border-b border-neutral-900 transition-colors ${active
            ? "bg-brand-500/10"
            : "hover:bg-neutral-900"}"
        >
          <td class="px-3 py-2 font-mono text-brand-300">${w.project}</td>
          <td class="px-3 py-2 font-mono text-neutral-500">${w.seq}</td>
          <td class="px-3 py-2"><${MethodBadge} method=${w.method} /></td>
          <td class="px-3 py-2 font-mono text-neutral-300">${w.path}</td>
          <td class="px-3 py-2 text-neutral-500">${fmtTime(w.received_at)}</td>
          <td class="px-3 py-2 text-right font-mono text-neutral-500">
            ${fmtBytes(w.body_len)}
          </td>
        </tr>`;
      })}
    </tbody>
  </table>`;
}
