// Dropdown is a fully CSS-drawn replacement for native <select>. WebKitGTK
// renders native option popups with the OS GTK theme — white menus on light
// systems, tiny text, no way to restyle from CSS — so every dropdown in the
// app goes through this component instead. Menus render at document.body so
// card and pane overflow rules cannot clip them. The component keeps the
// native select callback shape (onChange receives {target:{value}}).
import { html, render } from "../vendor/preact/index.js";
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
  disabled = false,
}) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1); // keyboard-highlighted index
  const [menuStyle, setMenuStyle] = useState(null);
  const rootRef = useRef(null);
  const portalRef = useRef(null);

  const items = normalize(options);
  const selectedIndex = items.findIndex((o) => o.value === value);
  const current = items[selectedIndex] || items[0] || { value: "", label: "—" };
  const unavailable = disabled || items.length === 0;

  useEffect(() => {
    if (unavailable && open) setOpen(false);
  }, [unavailable, open]);

  const positionMenu = () => {
    const trigger = rootRef.current?.querySelector(".dropdown-trigger");
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const edge = 8;
    const gap = 4;
    const below = window.innerHeight - rect.bottom - gap - edge;
    const above = rect.top - gap - edge;
    const openUp = below < 160 && above > below;
    const availableHeight = Math.max(80, openUp ? above : below);
    const width = Math.min(Math.max(rect.width, 160), window.innerWidth - edge * 2);
    const left = Math.min(Math.max(rect.left, edge), window.innerWidth - width - edge);
    setMenuStyle({
      position: "fixed",
      left: `${left}px`,
      right: "auto",
      top: openUp ? "auto" : `${rect.bottom + gap}px`,
      bottom: openUp ? `${window.innerHeight - rect.top + gap}px` : "auto",
      width: `${width}px`,
      maxHeight: `${Math.min(280, availableHeight)}px`,
    });
  };

  // Close on outside click / Escape while open.
  useEffect(() => {
    if (!open) return undefined;
    const onDown = (e) => {
      const insideTrigger = rootRef.current?.contains(e.target);
      const insideMenu = portalRef.current?.contains(e.target);
      if (!insideTrigger && !insideMenu) setOpen(false);
    };
    const onFocus = (e) => {
      const insideTrigger = rootRef.current?.contains(e.target);
      const insideMenu = portalRef.current?.contains(e.target);
      if (!insideTrigger && !insideMenu) setOpen(false);
    };
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("focusin", onFocus);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("focusin", onFocus);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open]);

  // Render the menu at the document root. An absolutely-positioned child can
  // never escape an ancestor with overflow hidden, regardless of z-index.
  useEffect(() => {
    if (!open) return undefined;
    const portal = document.createElement("div");
    portal.className = "dropdown-portal";
    document.body.appendChild(portal);
    portalRef.current = portal;
    positionMenu();
    window.addEventListener("resize", positionMenu);
    window.addEventListener("scroll", positionMenu, true);
    return () => {
      window.removeEventListener("resize", positionMenu);
      window.removeEventListener("scroll", positionMenu, true);
      render(null, portal);
      portal.remove();
      portalRef.current = null;
    };
  }, [open]);

  const pick = (v) => {
    setOpen(false);
    if (onChange) onChange({ target: { value: v } });
  };

  const onTriggerKey = (e) => {
    if (unavailable) return;
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

  useEffect(() => {
    if (!open || !portalRef.current || !menuStyle) return;
    render(html`<div class="dropdown-menu dropdown-menu-portal" role="listbox" aria-label=${ariaLabel} style=${menuStyle}>
      ${items.map(
        (o, i) => html`<button
          type="button"
          key=${o.value}
          role="option"
          aria-selected=${o.value === value}
          title=${o.label}
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
    </div>`, portalRef.current);
  });

  return html`<div class="dropdown ${open ? "open" : ""} ${unavailable ? "disabled" : ""} ${cls}" ref=${rootRef}>
    <button
      type="button"
      class="dropdown-trigger"
      disabled=${unavailable}
      aria-haspopup="listbox"
      aria-expanded=${open}
      aria-label=${ariaLabel}
      title=${current.label}
      onClick=${() => {
        if (unavailable) return;
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
  </div>`;
}
