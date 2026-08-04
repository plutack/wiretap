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

  return html`<div class="border-b border-neutral-800 px-2 py-1.5">
    <input
      type="search"
      placeholder="Search method, path, url…"
      value=${value}
      onInput=${(e) => setValue(e.target.value)}
      class="w-full rounded border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm"
    />
  </div>`;
}
