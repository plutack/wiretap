// Small presentational helpers shared across list/detail components. These
// return Preact vnodes (via htm) rather than HTML strings, so callers compose
// them directly in template literals.
import { html } from "../vendor/preact/index.js";
import { methodBadgeClass, statusColorClass } from "../lib/format.js";

export function MethodBadge({ method }) {
  return html`<span
    class="rounded px-1.5 py-0.5 font-mono text-[11px] ${methodBadgeClass(method)}"
    >${method || ""}</span
  >`;
}

export function StatusBadge({ status }) {
  if (!status) return html`<span class="text-neutral-600">—</span>`;
  return html`<span class="font-mono ${statusColorClass(status)}">${status}</span>`;
}

// HeaderTable renders a header map ({name: [values]}) as a two-column table.
export function HeaderTable({ headers }) {
  const entries = Object.entries(headers || {});
  if (entries.length === 0)
    return html`<p class="text-neutral-600">(no headers)</p>`;
  return html`<table class="w-full text-xs">
    <tbody>
      ${entries.map(
        ([k, vs]) => html`<tr>
          <td class="align-top pr-3 font-mono text-neutral-400">${k}</td>
          <td class="font-mono break-all">
            ${Array.isArray(vs) ? vs.join(", ") : String(vs)}
          </td>
        </tr>`,
      )}
    </tbody>
  </table>`;
}
