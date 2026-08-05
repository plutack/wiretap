import { html } from "../vendor/preact/index.js";
import { useEffect, useRef, useState } from "../vendor/preact/index.js";

export function SearchBar({ onSearch, placeholder = "Filter the current signal stream…" }) {
  const [value, setValue] = useState("");
  const inputRef = useRef(null);
  const timer = useRef(null);

  useEffect(() => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => onSearch(value.trim()), 300);
    return () => clearTimeout(timer.current);
  }, [value]);

  useEffect(() => {
    const shortcut = (event) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        inputRef.current?.focus();
      }
      if (event.key === "Escape" && document.activeElement === inputRef.current) {
        setValue("");
        inputRef.current.blur();
      }
    };
    window.addEventListener("keydown", shortcut);
    return () => window.removeEventListener("keydown", shortcut);
  }, []);

  return html`<div class="search-shell">
    <span class="search-icon" aria-hidden="true">
      <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round">
        <circle cx="8.5" cy="8.5" r="5.5" />
        <path d="m13 13 4 4" />
      </svg>
    </span>
    <input
      ref=${inputRef}
      type="search"
      value=${value}
      placeholder=${placeholder}
      onInput=${(event) => setValue(event.target.value)}
      aria-label="Filter signal stream"
    />
    ${value
      ? html`<button
          class="search-clear"
          onClick=${() => setValue("")}
          aria-label="Clear filter"
        >×</button>`
      : html`<span class="search-key">Ctrl K</span>`}
  </div>`;
}
