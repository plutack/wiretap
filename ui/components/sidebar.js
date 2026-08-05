// Sidebar renders project filters, script list, and client-side filter controls
// (method, status range). It's a pure view over state passed down from the root.
import { html } from "../vendor/preact/index.js";
import { Section, Select, Field } from "./ui.js";

const METHOD_OPTIONS = [
  { value: "", label: "All methods" },
  { value: "GET", label: "GET" },
  { value: "POST", label: "POST" },
  { value: "PUT", label: "PUT" },
  { value: "PATCH", label: "PATCH" },
  { value: "DELETE", label: "DELETE" },
];

const STATUS_OPTIONS = [
  { value: "", label: "All statuses" },
  { value: "2xx", label: "2xx · success" },
  { value: "3xx", label: "3xx · redirect" },
  { value: "4xx", label: "4xx · client error" },
  { value: "5xx", label: "5xx · server error" },
];

function ProjectButton({ label, active, onClick }) {
  return html`<button
    onClick=${onClick}
    class="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-sm transition-colors ${active
      ? "bg-brand-500/15 text-brand-200 ring-1 ring-brand-500/30"
      : "text-neutral-300 hover:bg-neutral-800"}"
  >
    <span class="h-1.5 w-1.5 flex-shrink-0 rounded-full ${active ? "bg-brand-400" : "bg-neutral-600"}"></span>
    <span class="min-w-0 flex-1 truncate">${label}</span>
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
  return html`<aside
    class="flex w-60 flex-shrink-0 flex-col overflow-y-auto border-r border-neutral-800 bg-neutral-900/30"
  >
    <${Section} title="Projects" class="border-b border-neutral-800 p-3">
      <div class="space-y-0.5">
        <${ProjectButton}
          label="All projects"
          active=${selectedProject === ""}
          onClick=${() => onSelectProject("")}
        />
        ${projects.map(
          (p) => html`<${ProjectButton}
            key=${p}
            label=${p}
            active=${selectedProject === p}
            onClick=${() => onSelectProject(p)}
          />`,
        )}
      </div>
    </>

    <${Section}
      title="Scripts"
      class="border-b border-neutral-800 p-3"
      action=${html`<button
        onClick=${onNewScript}
        class="btn btn-primary btn-xs"
      >
        + New
      </button>`}
    >
      ${scripts.length === 0
        ? html`<p class="px-1 py-2 text-xs text-neutral-600">No scripts yet.</p>`
        : html`<div class="space-y-0.5">
            ${scripts.map(
              (s) => html`<div
                key=${s.id}
                class="group flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-neutral-800"
              >
                <input
                  type="checkbox"
                  checked=${s.enabled}
                  onChange=${(e) => onScriptToggle(s.id, e.target.checked)}
                  class="checkbox"
                  title=${s.enabled ? "Enabled" : "Disabled"}
                />
                <button
                  onClick=${() => onScriptSelect(s)}
                  class="min-w-0 flex-1 truncate text-left ${s.enabled ? "text-neutral-200" : "text-neutral-500"}"
                >
                  ${s.name || "(unnamed)"}
                </button>
                <span class="chip bg-neutral-800 text-neutral-500 opacity-0 group-hover:opacity-100">
                  ${(s.trigger || "").replace("on_", "")}
                </span>
              </div>`,
            )}
          </div>`}
    </>

    <${Section} title="Filters" class="p-3">
      <div class="space-y-3">
        <${Field} label="Method">
          <${Select}
            value=${methodFilter}
            onChange=${(e) => onMethodFilterChange(e.target.value)}
            options=${METHOD_OPTIONS}
          />
        </>
        <${Field} label="Status">
          <${Select}
            value=${statusFilter}
            onChange=${(e) => onStatusFilterChange(e.target.value)}
            options=${STATUS_OPTIONS}
          />
        </>
      </div>
    </>
  </aside>`;
}
