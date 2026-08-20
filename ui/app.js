// wiretap GUI root. Composes the Preact + htm component tree, owns app state
// (active tab, polled status, fetched rows, filters, selection) and wires the
// Wails bindings through lib/api.js. No build step: everything loads as ES
// modules, htm provides the tagged-template markup.
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
import { Settings } from "./components/settings.js";

function Toast({ message }) {
  if (!message) return null;
  return html`<div class="workbench-toast">${message}</div>`;
}

function CommandDeck({ activeTab, onChange, counts, onSearch, project, filtered }) {
  const tab = (id, label, glyph) => html`<button
    onClick=${() => onChange(id)}
    class="mode-tab ${activeTab === id ? "active" : ""}"
  >
    <span aria-hidden="true">${glyph}</span>
    <span>${label}</span>
    <span class="mode-count">${counts[id] ?? 0}</span>
  </button>`;

  return html`<div class="command-deck">
    <div class="command-topline">
      <nav class="mode-tabs" aria-label="Signal stream">
        ${tab("webhooks", "Ingress", "↘")}
        ${tab("traffic", "Traffic", "⇄")}
      </nav>
      <${SearchBar}
        onSearch=${onSearch}
        placeholder=${activeTab === "webhooks" ? "Filter source, method, or route…" : "Filter method, host, or URL…"}
      />
      <div class="workspace-summary">
        ${project ? `source:${project} · ` : ""}${filtered} visible
      </div>
    </div>
  </div>`;
}

function App() {
  const [activeTab, setActiveTab] = useState("webhooks");
  const [status, setStatus] = useState(null);
  const [webhooks, setWebhooks] = useState([]);
  const [captures, setCaptures] = useState([]);
  const [scripts, setScripts] = useState([]);

  const [project, setProject] = useState("");
  const [sessions, setSessions] = useState([]);
  const [sessionFilter, setSessionFilter] = useState(0); // 0 = all sessions
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
      setCaptures((await api.listCaptures(sessionFilter)) || []);
    } catch (e) {
      showToast("list captures: " + e);
    }
  };
  const loadSessions = async () => {
    try {
      setSessions((await api.listSessions()) || []);
    } catch (e) {
      showToast("list sessions: " + e);
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
    if (activeTab === "settings") return undefined;
    const tick = () => {
      if (activeTab === "webhooks") {
        loadWebhooks();
      } else {
        loadCaptures();
        loadSessions();
      }
    };
    tick();
    const t = setInterval(tick, 2000);
    return () => clearInterval(t);
  }, [activeTab, project, sessionFilter]);

  useEffect(() => {
    if (!selection) return undefined;

    const closeOnEscape = (event) => {
      if (event.key === "Escape") setSelection(null);
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [selection]);

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
        defaultTarget=${(status && status.forward_url) || ""}
        onReplay=${api.replayWebhook}
        onExport=${(target, client) =>
          api.exportWebhook(selection.data.project, selection.data.seq, target, client)}
        onClose=${closeDetail}
      />`;
    if (selection.kind === "traffic")
      return html`<${TrafficDetail}
        capture=${selection.data}
        onExport=${(target, client) =>
          api.exportCapture(selection.data.id, target, client)}
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

  const changeTab = (tab) => {
    setActiveTab(tab);
    setSelection(null);
  };

  const toggleSettings = () => {
    setSelection(null);
    setActiveTab((tab) => (tab === "settings" ? "webhooks" : "settings"));
  };

  return html`<div class="workbench">
    <${StatusBar}
      status=${status}
      settingsActive=${activeTab === "settings"}
      onOpenSettings=${toggleSettings}
      onRefresh=${() => {
        loadStatus();
        loadScripts();
        if (activeTab === "webhooks") loadWebhooks();
        else if (activeTab === "traffic") loadCaptures();
      }}
    />
    <div class="workbench-body">
      <${Sidebar}
        projects=${(status && status.connected_projects) || []}
        selectedProject=${project}
        onSelectProject=${setProject}
        sessions=${sessions}
        selectedSession=${sessionFilter}
        onSelectSession=${(id) => {
          setSessionFilter(id);
          if (activeTab !== "traffic") changeTab("traffic");
        }}
        scripts=${scripts}
        onScriptToggle=${toggleScript}
        onScriptSelect=${openScript}
        onNewScript=${newScript}
        methodFilter=${methodFilter}
        onMethodFilterChange=${setMethodFilter}
        statusFilter=${statusFilter}
        onStatusFilterChange=${setStatusFilter}
      />
      <div class="workspace">
        ${activeTab === "settings"
          ? html`<main class="workspace-main full">
              <section class="event-stage">
                <${Settings} onToast=${showToast} onSaved=${loadStatus} />
              </section>
            </main>`
          : html`<${CommandDeck}
                activeTab=${activeTab}
                onChange=${changeTab}
                counts=${{ webhooks: visibleWebhooks.length, traffic: visibleCaptures.length }}
                onSearch=${setSearch}
                project=${project}
                filtered=${activeTab === "webhooks" ? visibleWebhooks.length : visibleCaptures.length}
              />
              <main class="workspace-main">
                <section class="event-stage">
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
              </main>`}
      </div>
    </div>
    <${Toast} message=${toast} />
  </div>`;
}

render(html`<${App} />`, document.getElementById("app"));
