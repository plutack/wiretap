// WebhookList renders a table of webhook summaries (project, seq, method, path,
// received time, body length). Clicking a row calls onSelect with (project, seq).
import { html } from "../vendor/preact/index.js";
import { MethodBadge } from "./badges.js";
import { fmtTime, fmtBytes } from "../lib/format.js";

export function WebhookList({ webhooks, onSelect }) {
  if (webhooks.length === 0) {
    return html`<p class="px-4 py-8 text-sm text-neutral-500">
      No webhooks captured yet.
    </p>`;
  }

  return html`<table class="w-full text-sm">
    <thead
      class="sticky top-0 bg-neutral-900 text-left text-xs uppercase text-neutral-500"
    >
      <tr>
        <th class="px-3 py-1.5">Project</th>
        <th class="px-3 py-1.5">Seq</th>
        <th class="px-3 py-1.5">Method</th>
        <th class="px-3 py-1.5">Path</th>
        <th class="px-3 py-1.5">Received</th>
        <th class="px-3 py-1.5 text-right">Body</th>
      </tr>
    </thead>
    <tbody class="divide-y divide-neutral-900">
      ${webhooks.map(
        (w) => html`<tr
          key="${w.project}-${w.seq}"
          onClick=${() => onSelect(w.project, w.seq)}
          class="cursor-pointer hover:bg-neutral-900"
        >
          <td class="px-3 py-1.5 font-mono text-sky-300">${w.project}</td>
          <td class="px-3 py-1.5 font-mono text-neutral-400">${w.seq}</td>
          <td class="px-3 py-1.5"><${MethodBadge} method=${w.method} /></td>
          <td class="px-3 py-1.5 font-mono text-neutral-300">${w.path}</td>
          <td class="px-3 py-1.5 text-neutral-500">${fmtTime(w.received_at)}</td>
          <td class="px-3 py-1.5 text-right font-mono text-neutral-500">
            ${fmtBytes(w.body_len)}
          </td>
        </tr>`,
      )}
    </tbody>
  </table>`;
}
