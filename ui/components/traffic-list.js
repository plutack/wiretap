// TrafficList renders a table of captured HTTP exchanges (id, method, url,
// status, time, req/resp byte counts). Clicking a row calls onSelect(id).
import { html } from "../vendor/preact/index.js";
import { MethodBadge, StatusBadge } from "./badges.js";
import { EmptyState } from "./ui.js";
import { fmtTime } from "../lib/format.js";

export function TrafficList({ captures, onSelect, selectedId }) {
  if (captures.length === 0) {
    return html`<${EmptyState}
      icon="⇅"
      title="No traffic captured yet"
      hint="Start interception and route an app through the proxy to see exchanges here."
    />`;
  }

  return html`<table class="w-full border-collapse text-sm">
    <thead class="sticky top-0 z-10 bg-neutral-900 text-left">
      <tr class="text-[11px] font-semibold tracking-wider text-neutral-500 uppercase">
        <th class="px-3 py-2 font-semibold">#</th>
        <th class="px-3 py-2 font-semibold">Method</th>
        <th class="px-3 py-2 font-semibold">URL</th>
        <th class="px-3 py-2 font-semibold">Status</th>
        <th class="px-3 py-2 font-semibold">At</th>
        <th class="px-3 py-2 text-right font-semibold">Req / Resp</th>
      </tr>
    </thead>
    <tbody>
      ${captures.map((c) => {
        const active = c.id === selectedId;
        return html`<tr
          key=${c.id}
          onClick=${() => onSelect(c.id)}
          class="cursor-pointer border-b border-neutral-900 transition-colors ${active
            ? "bg-brand-500/10"
            : "hover:bg-neutral-900"}"
        >
          <td class="px-3 py-2 font-mono text-neutral-500">${c.id}</td>
          <td class="px-3 py-2"><${MethodBadge} method=${c.method} /></td>
          <td class="max-w-0 px-3 py-2 font-mono text-neutral-300">
            <span class="block truncate" title=${c.url}>${c.url}</span>
          </td>
          <td class="px-3 py-2"><${StatusBadge} status=${c.status} /></td>
          <td class="px-3 py-2 text-neutral-500">${fmtTime(c.at)}</td>
          <td class="px-3 py-2 text-right font-mono text-neutral-500">
            ${c.req_body_len} / ${c.resp_body_len}
          </td>
        </tr>`;
      })}
    </tbody>
  </table>`;
}
