// wiretap GUI root. Composes the Preact + htm component tree, owns app state
// (active tab, polled status, fetched rows, filters, selection) and wires the
// Wails bindings through lib/api.js. No build step: everything loads as ES
// modules, htm provides the tagged-template markup (see PLAN.md §6.3).
import { html, render } from "./vendor/preact/index.js";
import { useEffect, useRef, useState } from "./vendor/preact/index.js";
import { api } from "./lib/api.js";
import { StatusBar } from "./components/status-bar.js";
import { Sidebar } from "./components/sidebar.js";
import { SearchBar } from "./components/search-bar.js";
import { WebhookList } from "./components/webhook-list.js";
import { WebhookDetail } from "./components/webhook-detail.js";
import { TrafficList } from "./components/traffic-list.js";
import { TrafficDetail } from "./components/traffic-detail.js";
import { ScriptEditor } from "./components/script-editor.js";

function Toast({ message }) {
  if (!message) return null;
  return html`<div class="pointer-events-none fixed bottom-4 left-1/2 z-50 -translate-x-1/2">
    <div class="pointer-events-auto rounded-lg border border-neutral-700 bg-neutral-800 px-4 py-2 text-xs text-neutral-100 shadow-lg shadow-black/40">
      ${message}
    </div>
  </div>`;
}

function TabBar({ activeTab, onChange, counts }) {
  const tab = (id, label) => html`<button
    onClick=${() => onChange(id)}
    class="flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm transition-colors ${activeTab === id
      ? "border-brand-500 text-brand-300"
      : "border-transparent text-neutral-400 hover:text-neutral-200"}"
  >
    ${label}
    <span class="chip ${activeTab === id ? "bg-brand-500/15 text-brand-300" : "bg-neutral-800 text-neutral-500"}">
      ${counts[id] ?? 0}
    </span>
  </button>`;
  return html`<nav class="flex gap-1 border-b border-neutral-800 px-2">
    ${tab("webhooks", "Webhooks")} ${tab("traffic", "Traffic")}
  </nav>`;
}

function App() {
  const [activeTab, setActiveTab] = useState("webhooks");
  const [status, setStatus] = useState(null);
  const [webhooks, setWebhooks] = useState([]);
  const [captures, setCaptures] = useState([]);
  const [scripts, setScripts] = useState([]);

  const [project, setProject] = useState("");
  const [methodFilter, setMethodFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [search, setSearch] = useState("");

  // selection: {kind: "webhook"|"traffic"|"script", data}
  const [selection, setSelection] = useState(null);

  const [toast, setToast] = useState("");
  const toastTimer = useRef(null);
  const showToast = (msg, ms = 3000) => {
    setToast(msg);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(""), ms);
  };

  // --- data loaders ------------------------------------------------------
  const loadStatus = async () => {
    try {
      setStatus(await api.status());
    } catch (e) {
      showToast("status: " + e);
    }
  };
  const loadWebhooks = async () => {
    try {
      setWebhooks((await api.listWebhooks(project)) || []);
    } catch (e) {
      showToast("list webhooks: " + e);
    }
  };
  const loadCaptures = async () => {
    try {
      setCaptures((await api.listCaptures()) || []);
    } catch (e) {
      showToast("list captures: " + e);
    }
  };
  const loadScripts = async () => {
    try {
      setScripts((await api.listScripts()) || []);
    } catch (e) {
      showToast("list scripts: " + e);
    }
  };

  // Initial load + polling. Status every 5s, active-tab rows every 2s.
  useEffect(() => {
    loadStatus();
    loadScripts();
    const s = setInterval(loadStatus, 5000);
    return () => clearInterval(s);
  }, []);

  useEffect(() => {
    const tick = () => (activeTab === "webhooks" ? loadWebhooks() : loadCaptures());
    tick();
    const t = setInterval(tick, 2000);
    return () => clearInterval(t);
  }, [activeTab, project]);

  // --- selection handlers ------------------------------------------------
  const openWebhook = async (proj, seq) => {
    try {
      const w = await api.getWebhook(proj, seq);
      setSelection({ kind: "webhook", data: w });
    } catch (e) {
      showToast("get webhook: " + e);
    }
  };
  const openCapture = async (id) => {
    try {
      const c = await api.getCapture(id);
      setSelection({ kind: "traffic", data: c });
    } catch (e) {
      showToast("get capture: " + e);
    }
  };
  const openScript = async (s) => {
    try {
      const full = await api.getScript(s.id);
      setSelection({ kind: "script", data: full });
    } catch (e) {
      showToast("get script: " + e);
    }
  };
  const newScript = () =>
    setSelection({
      kind: "script",
      data: { id: 0, name: "", trigger: "on_request", body: "", priority: 0, enabled: true },
    });

  const toggleScript = async (id, enabled) => {
    try {
      await api.setScriptEnabled(id, enabled);
      loadScripts();
    } catch (e) {
      showToast("toggle script: " + e);
    }
  };
  const saveScript = async (input) => {
    const id = await api.saveScript(input);
    await loadScripts();
    showToast(input.id ? `Updated script ${id}` : `Created script ${id}`);
    return id;
  };
  const deleteScript = async (id) => {
    await api.deleteScript(id);
    await loadScripts();
    showToast(`Deleted script ${id}`);
  };

  // --- client-side filtering --------------------------------------------
  const matchesStatus = (code) => {
    if (!statusFilter) return true;
    if (!code) return false;
    return String(code)[0] === statusFilter[0];
  };
  const matchesSearch = (fields) =>
    !search || fields.some((f) => String(f || "").toLowerCase().includes(search.toLowerCase()));

  const visibleWebhooks = webhooks.filter(
    (w) =>
      (!methodFilter || (w.method || "").toUpperCase() === methodFilter) &&
      matchesSearch([w.project, w.method, w.path]),
  );
  const visibleCaptures = captures.filter(
    (c) =>
      (!methodFilter || (c.method || "").toUpperCase() === methodFilter) &&
      matchesStatus(c.status) &&
      matchesSearch([c.method, c.url]),
  );

  const closeDetail = () => setSelection(null);

  const detailPane = () => {
    if (!selection) return null;
    if (selection.kind === "webhook")
      return html`<${WebhookDetail}
        webhook=${selection.data}
        onReplay=${api.replayWebhook}
        onClose=${closeDetail}
      />`;
    if (selection.kind === "traffic")
      return html`<${TrafficDetail}
        capture=${selection.data}
        onClose=${closeDetail}
      />`;
    if (selection.kind === "script")
      return html`<${ScriptEditor}
        script=${selection.data}
        onSave=${saveScript}
        onDelete=${deleteScript}
        onTest=${api.testScript}
        onClose=${closeDetail}
      />`;
    return null;
  };

  return html`<div class="flex h-full flex-col">
    <${StatusBar}
      status=${status}
      onRefresh=${() => {
        loadStatus();
        loadScripts();
        activeTab === "webhooks" ? loadWebhooks() : loadCaptures();
      }}
    />
    <div class="flex min-h-0 flex-1">
      <${Sidebar}
        projects=${(status && status.connected_projects) || []}
        selectedProject=${project}
        onSelectProject=${setProject}
        scripts=${scripts}
        onScriptToggle=${toggleScript}
        onScriptSelect=${openScript}
        onNewScript=${newScript}
        methodFilter=${methodFilter}
        onMethodFilterChange=${setMethodFilter}
        statusFilter=${statusFilter}
        onStatusFilterChange=${setStatusFilter}
      />
      <div class="flex min-w-0 flex-1 flex-col">
        <${TabBar}
          activeTab=${activeTab}
          onChange=${setActiveTab}
          counts=${{ webhooks: visibleWebhooks.length, traffic: visibleCaptures.length }}
        />
        <${SearchBar} onSearch=${setSearch} />
        <main class="flex min-h-0 flex-1">
          <section class="min-w-0 flex-1 overflow-auto">
            ${activeTab === "webhooks"
              ? html`<${WebhookList}
                  webhooks=${visibleWebhooks}
                  onSelect=${openWebhook}
                  selectedKey=${selection && selection.kind === "webhook"
                    ? `${selection.data.project}-${selection.data.seq}`
                    : null}
                />`
              : html`<${TrafficList}
                  captures=${visibleCaptures}
                  onSelect=${openCapture}
                  selectedId=${selection && selection.kind === "traffic"
                    ? selection.data.id
                    : null}
                />`}
          </section>
          ${detailPane()}
        </main>
      </div>
    </div>
    <${Toast} message=${toast} />
  </div>`;
}

render(html`<${App} />`, document.getElementById("app"));
