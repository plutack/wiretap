import { html } from "../vendor/preact/index.js";
import { Dropdown } from "./dropdown.js";

// fmtSessionLabel renders "Aug 20 · 14:05" from an RFC3339 timestamp.
function fmtSessionLabel(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso || "unknown";
  const day = d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return `${day} · ${time}`;
}

const METHOD_OPTIONS = [
  { value: "", label: "All methods" },
  { value: "GET", label: "GET" },
  { value: "POST", label: "POST" },
  { value: "PUT", label: "PUT" },
  { value: "PATCH", label: "PATCH" },
  { value: "DELETE", label: "DELETE" },
];
const STATUS_OPTIONS = [
  { value: "", label: "All status families" },
  { value: "2xx", label: "2xx · success" },
  { value: "3xx", label: "3xx · redirect" },
  { value: "4xx", label: "4xx · client error" },
  { value: "5xx", label: "5xx · server error" },
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
  sessions = [],
  selectedSession = 0,
  onSelectSession,
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
          ? html`<p class="px-2 py-2 text-xs leading-relaxed text-neutral-600">
              Connect a relay to discover project sources.
            </p>`
          : null}
      </>

      <${NavSection} title="Sessions" count=${sessions.length}>
        <button
          class="nav-item ${selectedSession === 0 ? "active" : ""}"
          onClick=${() => onSelectSession && onSelectSession(0)}
        >
          <span class="nav-glyph">∞</span>
          <span class="nav-label">All traffic</span>
        </button>
        ${sessions.map(
          (s) => html`<button
            key=${s.id}
            class="nav-item ${selectedSession === s.id ? "active" : ""}"
            title=${`session #${s.id} · ${s.shell || "shell"} · ${s.proxy_addr || ""}`}
            onClick=${() => onSelectSession && onSelectSession(s.id)}
          >
            <span class="nav-glyph">#${s.id}</span>
            <span class="nav-label">${fmtSessionLabel(s.started_at)}</span>
            ${!s.ended_at
              ? html`<span class="live-dot online" title="running"></span>`
              : html`<span class="nav-count">${s.captures}</span>`}
          </button>`,
        )}
        ${sessions.length === 0
          ? html`<p class="px-2 py-2 text-xs leading-relaxed text-neutral-600">
              Run <code>wiretap intercept start</code> to record a session.
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
        <div class="mb-2">
          <${Dropdown}
            aria-label="Filter by method"
            value=${methodFilter}
            onChange=${(event) => onMethodFilterChange(event.target.value)}
            options=${METHOD_OPTIONS}
          />
        </div>
        <div class="mb-2">
          <${Dropdown}
            aria-label="Filter by status"
            value=${statusFilter}
            onChange=${(event) => onStatusFilterChange(event.target.value)}
            options=${STATUS_OPTIONS}
          />
        </div>
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
