import { html } from "../vendor/preact/index.js";

const METHOD_OPTIONS = ["", "GET", "POST", "PUT", "PATCH", "DELETE"];
const STATUS_OPTIONS = [
  ["", "All status families"],
  ["2xx", "2xx · success"],
  ["3xx", "3xx · redirect"],
  ["4xx", "4xx · client error"],
  ["5xx", "5xx · server error"],
];

function NavSection({ title, count, action, children }) {
  return html`<section class="nav-section">
    <div class="nav-heading">
      <span>${title}</span>
      ${action || (count != null ? html`<span class="nav-count">${count}</span>` : null)}
    </div>
    ${children}
  </section>`;
}

function ProjectItem({ label, active, onClick, all = false }) {
  return html`<button class="nav-item ${active ? "active" : ""}" onClick=${onClick}>
    <span class="nav-glyph">${all ? "∞" : label.slice(0, 2).toUpperCase()}</span>
    <span class="nav-label">${label}</span>
    ${active ? html`<span class="live-dot online"></span>` : null}
  </button>`;
}

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
  const enabledScripts = scripts.filter((script) => script.enabled).length;

  return html`<aside class="navigator">
    <div class="navigator-scroll">
      <${NavSection} title="Sources" count=${projects.length}>
        <${ProjectItem}
          label="All projects"
          all=${true}
          active=${selectedProject === ""}
          onClick=${() => onSelectProject("")}
        />
        ${projects.map(
          (project) => html`<${ProjectItem}
            key=${project}
            label=${project}
            active=${selectedProject === project}
            onClick=${() => onSelectProject(project)}
          />`,
        )}
        ${projects.length === 0
          ? html`<p class="px-2 py-2 text-[10px] leading-relaxed text-neutral-600">
              Connect a relay to discover project sources.
            </p>`
          : null}
      </>

      <${NavSection}
        title="Transforms"
        count=${enabledScripts + "/" + scripts.length}
        action=${html`<button class="new-script-button" title="New transform" aria-label="New transform" onClick=${onNewScript}>＋</button>`}
      >
        ${scripts.map(
          (script) => html`<div key=${script.id} class="nav-item">
            <input
              class="script-switch"
              type="checkbox"
              checked=${script.enabled}
              title=${script.enabled ? "Disable transform" : "Enable transform"}
              onChange=${(event) => onScriptToggle(script.id, event.target.checked)}
            />
            <button
              class="nav-label nav-label-button"
              title=${script.name || "Unnamed transform"}
              onClick=${() => onScriptSelect(script)}
            >
              ${script.name || "Unnamed transform"}
            </button>
            <span class="nav-count">${(script.trigger || "").replace("on_", "")}</span>
          </div>`,
        )}
        ${scripts.length === 0
          ? html`<button class="nav-item" onClick=${onNewScript}>
              <span class="nav-glyph">JS</span>
              <span class="nav-label">Create first transform</span>
            </button>`
          : null}
      </>

      <${NavSection} title="Lens">
        <select
          class="nav-select"
          aria-label="Filter by method"
          value=${methodFilter}
          onChange=${(event) => onMethodFilterChange(event.target.value)}
        >
          ${METHOD_OPTIONS.map(
            (method) => html`<option key=${method} value=${method}>${method || "All methods"}</option>`,
          )}
        </select>
        <select
          class="nav-select"
          aria-label="Filter by status"
          value=${statusFilter}
          onChange=${(event) => onStatusFilterChange(event.target.value)}
        >
          ${STATUS_OPTIONS.map(
            ([value, label]) => html`<option key=${value} value=${value}>${label}</option>`,
          )}
        </select>
        ${(methodFilter || statusFilter)
          ? html`<button
              class="nav-item"
              onClick=${() => {
                onMethodFilterChange("");
                onStatusFilterChange("");
              }}
            >
              <span class="nav-glyph">×</span>
              <span class="nav-label">Clear lens</span>
            </button>`
          : null}
      </>
    </div>

    <footer class="navigator-footer">
      <div>${projects.length ? `${projects.length} relay source${projects.length === 1 ? "" : "s"}` : "local mode"}</div>
      <div>${enabledScripts} active transform${enabledScripts === 1 ? "" : "s"}</div>
    </footer>
  </aside>`;
}
