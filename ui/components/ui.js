// Shared presentational primitives. Centralizing buttons, inputs, selects,
// fields and section headers here (paired with the .btn/.input/.select classes
// in input.css) is what keeps the UI visually consistent — components compose
// these instead of hand-rolling Tailwind class strings, so a dropdown looks the
// same everywhere and nothing ends up "barely visible".
import { html } from "../vendor/preact/index.js";
import { Dropdown } from "./dropdown.js";

/** Button with a visual variant: "primary" | "ghost" | "danger". */
export function Button({ variant = "ghost", type = "button", class: cls = "", children, ...rest }) {
  const v = { primary: "btn-primary", ghost: "btn-ghost", danger: "btn-danger" }[variant] || "btn-ghost";
  return html`<button type=${type} class="btn ${v} ${cls}" ...${rest}>${children}</button>`;
}

/** Small square icon button (used for close/refresh affordances). */
export function IconButton({ label, class: cls = "", children, ...rest }) {
  return html`<button aria-label=${label} title=${label} class="btn-icon ${cls}" ...${rest}>
    ${children}
  </button>`;
}

/** Text input. Forwards all native props (value, onInput, placeholder, …). */
export function Input({ class: cls = "", ...rest }) {
  return html`<input class="input ${cls}" ...${rest} />`;
}

/**
 * Select is the app-wide dropdown. It used to be a styled native <select>,
 * but WebKitGTK draws native option popups with the OS GTK theme (white on
 * light desktops), so it now delegates to the CSS-drawn Dropdown while
 * keeping the same props/callback shape.
 * @param options array of {value, label} or plain strings.
 */
export function Select({ value, onChange, options = [], class: cls = "", ...rest }) {
  return html`<${Dropdown}
    value=${value}
    onChange=${onChange}
    options=${options}
    class=${cls}
    ...${rest}
  />`;
}

/** Labelled form field wrapper: a small caption stacked above its control. */
export function Field({ label, children, class: cls = "" }) {
  return html`<label class="block ${cls}">
    <span class="mb-1 block text-xs font-medium text-neutral-400">${label}</span>
    ${children}
  </label>`;
}

/** Section with an uppercase label header. */
export function Section({ title, action, children, class: cls = "" }) {
  return html`<section class=${cls}>
    <div class="mb-2 flex items-center justify-between">
      <h3 class="section-label">${title}</h3>
      ${action || null}
    </div>
    ${children}
  </section>`;
}

/** Empty-state placeholder centered in its container. */
export function EmptyState({ icon, title, hint }) {
  return html`<div class="flex h-full flex-col items-center justify-center gap-2 px-6 py-16 text-center">
    ${icon ? html`<div class="text-3xl opacity-40">${icon}</div>` : null}
    <p class="text-sm font-medium text-neutral-400">${title}</p>
    ${hint ? html`<p class="max-w-xs text-xs text-neutral-600">${hint}</p>` : null}
  </div>`;
}
