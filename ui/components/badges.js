// Small presentational helpers shared across list/detail components. These
// return Preact vnodes (via htm) rather than HTML strings, so callers compose
// them directly in template literals.
import { html } from "../vendor/preact/index.js";
import { methodBadgeClass, statusColorClass } from "../lib/format.js";

export function MethodBadge({ method }) {
  return html`<span class="chip ${methodBadgeClass(method)}">${method || ""}</span>`;
}

export function StatusBadge({ status }) {
  if (!status) return html`<span class="text-neutral-600">—</span>`;
  return html`<span class="font-mono text-sm ${statusColorClass(status)}">${status}</span>`;
}

// HeaderTable renders a header map ({name: [values]}) as a two-column table.
export function HeaderTable({ headers }) {
  const entries = Object.entries(headers || {});
  if (entries.length === 0)
    return html`<p class="rounded-md border border-dashed border-neutral-800 px-3 py-2 text-xs text-neutral-600">
      (no headers)
    </p>`;
  return html`<div class="overflow-hidden rounded-md border border-neutral-800">
    <table class="w-full text-xs">
      <tbody class="divide-y divide-neutral-800/70">
        ${entries.map(
          ([k, vs]) => html`<tr key=${k} class="align-top">
            <td class="w-1/3 bg-neutral-900/40 px-2.5 py-1.5 font-mono text-neutral-400 break-all">
              ${k}
            </td>
            <td class="px-2.5 py-1.5 font-mono text-neutral-200 break-all">
              ${Array.isArray(vs) ? vs.join(", ") : String(vs)}
            </td>
          </tr>`,
        )}
      </tbody>
    </table>
  </div>`;
}
