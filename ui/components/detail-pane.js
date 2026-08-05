import { html } from "../vendor/preact/index.js";
import { IconButton } from "./ui.js";

export function DetailPane({ title, onClose, headerExtra, children, class: cls = "" }) {
  return html`<aside class="detail-pane ${cls}">
    <div class="inspector-head">
      <div class="inspector-title">${title}</div>
      ${headerExtra || null}
      <${IconButton} label="Close inspector" class="ml-auto" onClick=${onClose}>
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round">
          <path d="m5 5 10 10M15 5 5 15" />
        </svg>
      </>
    </div>
    ${children}
  </aside>`;
}

export function DetailBody({ children }) {
  return html`<div class="inspector-body">${children}</div>`;
}

export function BodySection({ title, len, children }) {
  return html`<section class="inspector-section">
    <div class="inspector-label">
      <span>${title}</span>
      ${len ? html`<span class="font-mono normal-case tracking-normal text-neutral-600">${len}</span>` : null}
    </div>
    ${children}
  </section>`;
}
