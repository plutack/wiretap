// CommandPalette (Ctrl/Cmd+K): fuzzy-filtered action list over everything the
// workbench can do — switch streams, filter by project/session, open the
// transform editor or settings, and act on the current selection (copy as
// curl, replay). Actions are supplied by app.js so the palette stays a dumb,
// reusable overlay.
import { html } from "../vendor/preact/index.js";
import { useEffect, useMemo, useRef, useState } from "../vendor/preact/index.js";

// match: simple subsequence match ("tse" hits "Traffic: session…"), ranked by
// contiguous-substring first. Cheap and dependency-free.
function match(query, text) {
  const q = query.toLowerCase();
  const t = text.toLowerCase();
  if (!q) return 1;
  if (t.includes(q)) return 2;
  let qi = 0;
  for (let ti = 0; ti < t.length && qi < q.length; ti++) {
    if (t[ti] === q[qi]) qi++;
  }
  return qi === q.length ? 1 : 0;
}

export function CommandPalette({ actions, onClose }) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef(null);
  const listRef = useRef(null);

  useEffect(() => {
    inputRef.current && inputRef.current.focus();
  }, []);

  const visible = useMemo(() => {
    const scored = actions
      .map((a) => ({ a, score: match(query, a.label + " " + (a.hint || "")) }))
      .filter((x) => x.score > 0);
    scored.sort((x, y) => y.score - x.score);
    return scored.map((x) => x.a).slice(0, 40);
  }, [actions, query]);

  useEffect(() => {
    setActive(0);
  }, [query]);

  const run = (action) => {
    onClose();
    // Run after close so toasts/navigation land on the workbench, not the overlay.
    Promise.resolve().then(() => action.run());
  };

  const onKey = (e) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((i) => Math.min(visible.length - 1, i + 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => Math.max(0, i - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (visible[active]) run(visible[active]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  };

  // Keep the active row in view while arrowing.
  useEffect(() => {
    const el = listRef.current && listRef.current.children[active];
    el && el.scrollIntoView({ block: "nearest" });
  }, [active]);

  return html`<div class="palette-overlay" onMouseDown=${(e) => e.target === e.currentTarget && onClose()}>
    <div class="palette" role="dialog" aria-label="Command palette">
      <input
        ref=${inputRef}
        class="palette-input"
        placeholder="Jump to, filter, or act…"
        value=${query}
        onInput=${(e) => setQuery(e.target.value)}
        onKeyDown=${onKey}
      />
      <div class="palette-list" ref=${listRef}>
        ${visible.map(
          (a, i) => html`<button
            key=${a.id}
            class="palette-item ${i === active ? "active" : ""}"
            onMouseEnter=${() => setActive(i)}
            onClick=${() => run(a)}
          >
            <span class="palette-glyph">${a.glyph || "›"}</span>
            <span>${a.label}</span>
            ${a.hint ? html`<span class="palette-hint">${a.hint}</span>` : null}
          </button>`,
        )}
        ${visible.length === 0
          ? html`<div class="palette-empty">No matching command</div>`
          : null}
      </div>
    </div>
  </div>`;
}
