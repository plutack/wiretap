// Shared chrome for the right-hand detail panels (webhook / traffic / script).
// Standardizing the header, close affordance, scroll container and section
// spacing here keeps all three panels visually identical.
import { html } from "../vendor/preact/index.js";
import { IconButton } from "./ui.js";

export function DetailPane({ title, onClose, headerExtra, children, class: cls = "" }) {
  return html`<aside
    class="flex w-1/2 min-w-[30rem] flex-col overflow-hidden border-l border-neutral-800 bg-neutral-900/40 ${cls}"
  >
    <div class="flex items-center gap-2 border-b border-neutral-800 px-4 py-2.5">
      <h2 class="text-sm font-semibold text-neutral-100">${title}</h2>
      ${headerExtra || null}
      <${IconButton} label="Close" class="ml-auto" onClick=${onClose}>✕</>
    </div>
    ${children}
  </aside>`;
}

/** Scrollable body container with consistent padding + vertical rhythm. */
export function DetailBody({ children }) {
  return html`<div class="flex-1 space-y-4 overflow-auto px-4 py-4 text-sm">
    ${children}
  </div>`;
}

/** A titled body section with an optional byte-length caption. */
export function BodySection({ title, len, children }) {
  return html`<section>
    <div class="mb-1.5 flex items-baseline gap-2">
      <h3 class="section-label">${title}</h3>
      ${len ? html`<span class="text-[11px] text-neutral-600">${len}</span>` : null}
    </div>
    ${children}
  </section>`;
}
