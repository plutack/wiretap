// Dropdown is a fully CSS-drawn replacement for native <select>. WebKitGTK
// renders native option popups with the OS GTK theme — white menus on light
// systems, tiny text, no way to restyle from CSS — so every dropdown in the
// app goes through this component instead. It keeps the native select's
// callback shape (onChange receives {target:{value}}) so existing callers
// don't change.
import { html } from "../vendor/preact/index.js";
import { useEffect, useRef, useState } from "../vendor/preact/index.js";

function normalize(options) {
  return options.map((o) =>
    typeof o === "string" ? { value: o, label: o === "" ? "—" : o } : o,
  );
}

export function Dropdown({
  value,
  options = [],
  onChange,
  class: cls = "",
  "aria-label": ariaLabel,
}) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1); // keyboard-highlighted index
  const rootRef = useRef(null);

  const items = normalize(options);
  const selectedIndex = items.findIndex((o) => o.value === value);
  const current = items[selectedIndex] || items[0] || { value: "", label: "—" };

  // Close on outside click / Escape while open.
  useEffect(() => {
    if (!open) return undefined;
    const onDown = (e) => {
      if (rootRef.current && !rootRef.current.contains(e.target)) setOpen(false);
    };
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open]);

  const pick = (v) => {
    setOpen(false);
    if (onChange) onChange({ target: { value: v } });
  };

  const onTriggerKey = (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!open) {
        setOpen(true);
        setActive(selectedIndex >= 0 ? selectedIndex : 0);
        return;
      }
      const dir = e.key === "ArrowDown" ? 1 : -1;
      setActive((i) => Math.min(items.length - 1, Math.max(0, i + dir)));
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      if (open && active >= 0 && items[active]) pick(items[active].value);
      else {
        setOpen(true);
        setActive(selectedIndex >= 0 ? selectedIndex : 0);
      }
    }
  };

  return html`<div class="dropdown ${open ? "open" : ""} ${cls}" ref=${rootRef}>
    <button
      type="button"
      class="dropdown-trigger"
      aria-haspopup="listbox"
      aria-expanded=${open}
      aria-label=${ariaLabel}
      onClick=${() => {
        setOpen((o) => !o);
        setActive(selectedIndex >= 0 ? selectedIndex : 0);
      }}
      onKeyDown=${onTriggerKey}
    >
      <span class="dropdown-value">${current.label}</span>
      <svg class="dropdown-chevron" width="12" height="12" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M6 8l4 4 4-4" />
      </svg>
    </button>
    ${open
      ? html`<div class="dropdown-menu" role="listbox">
          ${items.map(
            (o, i) => html`<button
              type="button"
              key=${o.value}
              role="option"
              aria-selected=${o.value === value}
              class="dropdown-option ${o.value === value ? "selected" : ""} ${i === active ? "active" : ""}"
              onMouseEnter=${() => setActive(i)}
              onClick=${() => pick(o.value)}
            >
              <span class="dropdown-value">${o.label}</span>
              ${o.value === value
                ? html`<svg width="12" height="12" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 10 3.5 3.5L15 7" /></svg>`
                : null}
            </button>`,
          )}
        </div>`
      : null}
  </div>`;
}
