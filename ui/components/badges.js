// Small presentational helpers shared across list/detail components. These
// return Preact vnodes (via htm) rather than HTML strings, so callers compose
// them directly in template literals.
import { html } from "../vendor/preact/index.js";
import { methodBadgeClass, statusColorClass } from "../lib/format.js";
import { copyText } from "../lib/clipboard.js";

export function MethodBadge({ method }) {
  return html`<span class="method-token ${methodBadgeClass(method)}">${method || "—"}</span>`;
}

export function StatusBadge({ status }) {
  if (!status) return html`<span class="mono-dim">pending</span>`;
  return html`<span class="status-token ${statusColorClass(status)}">${status}</span>`;
}

function headerValues(values) {
  return Array.isArray(values) ? values : [String(values)];
}

export function serializeHeaders(headers) {
  return Object.entries(headers || {})
    .flatMap(([name, values]) => headerValues(values).map((value) => `${name}: ${value}`))
    .join("\n");
}

// HeaderTable keeps repeated header values on separate lines and offers native
// clipboard actions that work in both Wails and browser-based development.
export function HeaderTable({ headers }) {
  const entries = Object.entries(headers || {}).sort(([a], [b]) => a.localeCompare(b));
  if (entries.length === 0)
    return html`<p class="rounded-md border border-dashed border-neutral-800 px-3 py-2 text-xs text-neutral-600">
      (no headers)
    </p>`;
  const copy = async (value) => {
    try {
      await copyText(value);
    } catch (error) {
      console.error("copy headers:", error);
    }
  };

  return html`<div class="overflow-hidden rounded-md border border-neutral-800">
    <div class="flex justify-end border-b border-neutral-800 bg-neutral-900/60 px-2 py-1">
      <button
        type="button"
        title="Copy all headers"
        onClick=${() => copy(serializeHeaders(headers))}
        class="rounded px-1.5 py-0.5 text-[11px] text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200"
      >
        Copy all
      </button>
    </div>
    <table class="header-table w-full text-xs">
      <tbody class="divide-y divide-neutral-800/70">
        ${entries.map(
          ([k, vs]) => html`<tr key=${k} class="align-top">
            <th scope="row" class="header-name-cell bg-neutral-900/40 px-2.5 py-2 text-left font-mono font-normal text-neutral-400 break-words">
              <div class="flex items-start gap-1">
                <span class="min-w-0 flex-1">${k}</span>
                <button
                  type="button"
                  title=${`Copy ${k}`}
                  aria-label=${`Copy ${k} header`}
                  onClick=${() => copy(headerValues(vs).map((value) => `${k}: ${value}`).join("\n"))}
                  class="shrink-0 rounded px-1 text-[10px] text-neutral-600 hover:bg-neutral-800 hover:text-neutral-300"
                >
                  Copy
                </button>
              </div>
            </th>
            <td class="px-2.5 py-2 font-mono text-neutral-200 break-words">
              <div class="space-y-1">
                ${headerValues(vs).map(
                  (value, index) => html`<div key=${`${k}-${index}`} class="header-value whitespace-pre-wrap">${value}</div>`,
                )}
              </div>
            </td>
          </tr>`,
        )}
      </tbody>
    </table>
  </div>`;
}
