// TrafficList renders a table of captured HTTP exchanges (id, method, url,
// status, time, req/resp byte counts). Clicking a row calls onSelect(id).
import { html } from "../vendor/preact/index.js";
import { MethodBadge, StatusBadge } from "./badges.js";
import { fmtTime } from "../lib/format.js";

export function TrafficList({ captures, onSelect }) {
  if (captures.length === 0) {
    return html`<p class="px-4 py-8 text-sm text-neutral-500">
      No traffic captured yet.
    </p>`;
  }

  return html`<table class="w-full text-sm">
    <thead
      class="sticky top-0 bg-neutral-900 text-left text-xs uppercase text-neutral-500"
    >
      <tr>
        <th class="px-3 py-1.5">#</th>
        <th class="px-3 py-1.5">Method</th>
        <th class="px-3 py-1.5">URL</th>
        <th class="px-3 py-1.5">Status</th>
        <th class="px-3 py-1.5">At</th>
        <th class="px-3 py-1.5 text-right">Req / Resp</th>
      </tr>
    </thead>
    <tbody class="divide-y divide-neutral-900">
      ${captures.map(
        (c) => html`<tr
          key=${c.id}
          onClick=${() => onSelect(c.id)}
          class="cursor-pointer hover:bg-neutral-900"
        >
          <td class="px-3 py-1.5 font-mono text-neutral-500">${c.id}</td>
          <td class="px-3 py-1.5"><${MethodBadge} method=${c.method} /></td>
          <td class="px-3 py-1.5 font-mono text-neutral-300 break-all">
            ${c.url}
          </td>
          <td class="px-3 py-1.5"><${StatusBadge} status=${c.status} /></td>
          <td class="px-3 py-1.5 text-neutral-500">${fmtTime(c.at)}</td>
          <td class="px-3 py-1.5 text-right font-mono text-neutral-500">
            ${c.req_body_len} / ${c.resp_body_len}
          </td>
        </tr>`,
      )}
    </tbody>
  </table>`;
}
