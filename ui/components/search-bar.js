import { html } from "../vendor/preact/index.js";
import { useEffect, useRef, useState } from "../vendor/preact/index.js";

export function SearchBar({
  onSearch,
  resetToken = 0,
  placeholder = "Filter the current signal stream…",
  showBodySearch = false,
  bodySearch = false,
  onBodySearchChange,
}) {
  const [value, setValue] = useState("");
  const inputRef = useRef(null);
  const timer = useRef(null);

  useEffect(() => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => onSearch(value.trim()), 300);
    return () => clearTimeout(timer.current);
  }, [value]);

  useEffect(() => {
    setValue("");
  }, [resetToken]);

  useEffect(() => {
    const shortcut = (event) => {
      const target = event.target;
      const isEditing =
        target instanceof HTMLElement &&
        (target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName));

      if (event.key === "/" && !isEditing && !event.ctrlKey && !event.metaKey && !event.altKey) {
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

  return html`<div class="search-shell ${showBodySearch ? "has-body-search" : ""}">
    <span class="search-icon" aria-hidden="true">
      <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round">
        <circle cx="8.5" cy="8.5" r="5.5" />
        <path d="m13 13 4 4" />
      </svg>
    </span>
    <input
      ref=${inputRef}
      class="search-input"
      type="search"
      inputmode="search"
      value=${value}
      placeholder=${placeholder}
      spellcheck="false"
      onInput=${(event) => setValue(event.target.value)}
      aria-label="Filter signal stream"
    />
    <span class="search-actions">
      ${
        showBodySearch
          ? html`<label
              class="body-search-toggle ${bodySearch ? "active" : ""}"
              title="Also match request and response bodies. This can be slower because body content is read during the search."
            >
              <input
                type="checkbox"
                checked=${bodySearch}
                onChange=${(event) => onBodySearchChange?.(event.target.checked)}
              />
              <span>Bodies</span>
            </label>`
          : null
      }
      ${
        value
          ? html`<button
              type="button"
              class="search-clear"
              onClick=${() => {
                setValue("");
                inputRef.current?.focus();
              }}
              aria-label="Clear filter"
              title="Clear filter"
            >
              <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
                <path d="m4 4 8 8M12 4l-8 8" />
              </svg>
            </button>`
          : html`<kbd class="search-key" title="Focus search">/</kbd>`
      }
    </span>
  </div>`;
}
