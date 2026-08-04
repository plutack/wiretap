// Sidebar renders project filters, script list, and client-side filter controls
// (method, status range). It's a pure view over state passed down from the root.
import { html } from "../vendor/preact/index.js";

export function Sidebar({
  projects,
  selectedProject,
  onSelectProject,
  scripts,
  onScriptToggle,
  onScriptSelect,
  onNewScript,
  methodFilter,
  onMethodFilterChange,
  statusFilter,
  onStatusFilterChange,
}) {
  return html`<aside
    class="w-64 flex-shrink-0 overflow-auto border-r border-neutral-800 bg-neutral-900/40"
  >
    <!-- Projects section -->
    <section class="border-b border-neutral-800 p-3">
      <h3 class="mb-2 text-xs uppercase text-neutral-500">Projects</h3>
      <button
        onClick=${() => onSelectProject("")}
        class="mb-1 w-full rounded px-2 py-1 text-left text-sm ${selectedProject ===
        ""
          ? "bg-sky-900/60 text-sky-300"
          : "text-neutral-300 hover:bg-neutral-800"}"
      >
        All
      </button>
      ${projects.map(
        (p) => html`<button
          key=${p}
          onClick=${() => onSelectProject(p)}
          class="mb-1 w-full rounded px-2 py-1 text-left text-sm ${selectedProject ===
          p
            ? "bg-sky-900/60 text-sky-300"
            : "text-neutral-300 hover:bg-neutral-800"}"
        >
          ${p}
        </button>`,
      )}
    </section>

    <!-- Scripts section -->
    <section class="border-b border-neutral-800 p-3">
      <div class="mb-2 flex items-center justify-between">
        <h3 class="text-xs uppercase text-neutral-500">Scripts</h3>
        <button
          onClick=${onNewScript}
          class="rounded bg-sky-600 px-2 py-0.5 text-xs text-white hover:bg-sky-500"
        >
          + New
        </button>
      </div>
      ${scripts.length === 0
        ? html`<p class="text-xs text-neutral-600">No scripts yet.</p>`
        : scripts.map(
            (s) => html`<div
              key=${s.id}
              class="mb-1 flex items-center gap-2 rounded px-2 py-1 text-sm hover:bg-neutral-800"
            >
              <input
                type="checkbox"
                checked=${s.enabled}
                onChange=${(e) => onScriptToggle(s.id, e.target.checked)}
                class="flex-shrink-0"
              />
              <button
                onClick=${() => onScriptSelect(s)}
                class="min-w-0 flex-1 truncate text-left text-neutral-300"
              >
                ${s.name || "(unnamed)"}
              </button>
            </div>`,
          )}
    </section>

    <!-- Filters section -->
    <section class="p-3">
      <h3 class="mb-2 text-xs uppercase text-neutral-500">Filters</h3>
      <label class="mb-3 block text-xs">
        <span class="mb-1 block text-neutral-400">Method</span>
        <select
          value=${methodFilter}
          onChange=${(e) => onMethodFilterChange(e.target.value)}
          class="w-full rounded border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm"
        >
          <option value="">All</option>
          <option value="GET">GET</option>
          <option value="POST">POST</option>
          <option value="PUT">PUT</option>
          <option value="PATCH">PATCH</option>
          <option value="DELETE">DELETE</option>
        </select>
      </label>
      <label class="block text-xs">
        <span class="mb-1 block text-neutral-400">Status</span>
        <select
          value=${statusFilter}
          onChange=${(e) => onStatusFilterChange(e.target.value)}
          class="w-full rounded border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm"
        >
          <option value="">All</option>
          <option value="2xx">2xx (success)</option>
          <option value="3xx">3xx (redirect)</option>
          <option value="4xx">4xx (client error)</option>
          <option value="5xx">5xx (server error)</option>
        </select>
      </label>
    </section>
  </aside>`;
}
