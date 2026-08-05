// SearchBar is a debounced client-side search input. It calls onSearch with the
// trimmed query 300ms after the user stops typing (NOT FTS5 — the parent filters
// already-loaded rows by substring; PLAN.md §6.5's FTS5 needs a Go layer).
import { html } from "../vendor/preact/index.js";
import { useEffect, useRef, useState } from "../vendor/preact/index.js";

export function SearchBar({ onSearch }) {
  const [value, setValue] = useState("");
  const timer = useRef(null);

  useEffect(() => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => onSearch(value.trim()), 300);
    return () => clearTimeout(timer.current);
  }, [value]);

  return html`<div class="border-b border-neutral-800 px-3 py-2">
    <div class="relative">
      <span class="pointer-events-none absolute inset-y-0 left-2.5 flex items-center text-neutral-500">
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
          <circle cx="9" cy="9" r="6" />
          <path d="M14 14l3.5 3.5" />
        </svg>
      </span>
      <input
        type="search"
        placeholder="Search method, path, url…"
        value=${value}
        onInput=${(e) => setValue(e.target.value)}
        class="input pl-8"
      />
      ${value
        ? html`<button
            onClick=${() => setValue("")}
            class="absolute inset-y-0 right-2 flex items-center text-neutral-500 hover:text-neutral-300"
            aria-label="Clear search"
          >
            ✕
          </button>`
        : null}
    </div>
  </div>`;
}
