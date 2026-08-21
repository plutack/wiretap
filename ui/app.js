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
import { CommandPalette } from "./components/palette.js";
import { copyText } from "./lib/clipboard.js";
import { applyDisplayPrefs, loadDisplayPrefs } from "./lib/prefs.js";

applyDisplayPrefs(loadDisplayPrefs());

function Toast({ message }) {
  if (!message) return null;
  return html`<div class="workbench-toast">${message}</div>`;
}

function CommandDeck({ activeTab, onChange, counts, onSearch, project, filtered, followControl }) {
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
        ${followControl}
      </div>
    </div>
  </div>`;
}

// Row fingerprints. Every poll calls setState with a fresh array identity,
// which re-renders the whole tree — including any open detail pane, whose
// body viewer is expensive on large payloads. Returning the previous state
// identity when the rows did not change lets Preact skip that re-render.
// Webhook (project, seq) pairs and capture ids are immutable once stored,
// so these fingerprints cannot miss a real change.
const webhooksFingerprint = (rows) => rows.map((w) => `${w.project}/${w.seq}`).join("|");
const capturesFingerprint = (rows) => rows.map((c) => `${c.id}:${c.status}`).join("|");
const sessionsFingerprint = (rows) => rows.map((s) => `${s.id}:${s.captures}:${s.ended_at || ""}`).join("|");

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

  const [selection, setSelection] = useState(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [follow, setFollow] = useState(true);
  const [queuedCount, setQueuedCount] = useState(0);
  const [pausedRows, setPausedRows] = useState(null);
  const lastVisibleCount = useRef(0);

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
      const rows = (await api.listWebhooks(project)) || [];
      setWebhooks((prev) => (webhooksFingerprint(prev) === webhooksFingerprint(rows) ? prev : rows));
    } catch (e) {
      showToast("list webhooks: " + e);
    }
  };
  const loadCaptures = async () => {
    try {
      const rows = (await api.listCaptures(sessionFilter)) || [];
      setCaptures((prev) => (capturesFingerprint(prev) === capturesFingerprint(rows) ? prev : rows));
    } catch (e) {
      showToast("list captures: " + e);
    }
  };
  const loadSessions = async () => {
    try {
      const rows = (await api.listSessions()) || [];
      setSessions((prev) => (sessionsFingerprint(prev) === sessionsFingerprint(rows) ? prev : rows));
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

  // Status and transform metadata have their own slower refresh cadence.
  useEffect(() => {
    loadStatus();
    loadScripts();
    const s = setInterval(loadStatus, 5000);
    return () => clearInterval(s);
  }, []);

  // Keep both stream counts warm so switching tabs never starts from an empty
  // count. Settings pauses the stream polling because it has no live rows.
  useEffect(() => {
    if (activeTab === "settings") return undefined;
    const tick = () => {
      loadWebhooks();
      loadCaptures();
      loadSessions();
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

  useEffect(() => {
    const onKeyDown = (event) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setPaletteOpen(true);
      }
      if (event.key === "Escape") setPaletteOpen(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  useEffect(() => {
    const count = activeTab === "webhooks" ? webhooks.length : captures.length;
    if (count > lastVisibleCount.current && !follow) {
      setQueuedCount((n) => n + count - lastVisibleCount.current);
    }
    if (follow) setQueuedCount(0);
    lastVisibleCount.current = count;
  }, [webhooks, captures, activeTab, follow]);

  // --- selection handlers ------------------------------------------------
  // Rapid row clicking must not pile work onto the webview: each detail
  // fetch ships the full body payload and each render decodes it, so every
  // queued click used to add another multi-MB render on arrival. In-flight
  // requests are deduplicated per row, and responses that were superseded
  // by a newer click are dropped instead of rendered.
  const latestDetailReq = useRef(0);
  const inflightDetails = useRef(new Map());
  const fetchDetail = (key, load) => {
    const pending = inflightDetails.current;
    let p = pending.get(key);
    if (!p) {
      p = load().finally(() => pending.delete(key));
      pending.set(key, p);
    }
    return p;
  };
  const openWebhook = async (proj, seq) => {
    const req = ++latestDetailReq.current;
    try {
      const w = await fetchDetail(`${proj}/${seq}`, () => api.getWebhook(proj, seq));
      if (req === latestDetailReq.current) setSelection({ kind: "webhook", data: w });
    } catch (e) {
      if (req === latestDetailReq.current) showToast("get webhook: " + e);
    }
  };
  const openCapture = async (id) => {
    const req = ++latestDetailReq.current;
    try {
      const c = await fetchDetail(`c/${id}`, () => api.getCapture(id));
      if (req === latestDetailReq.current) setSelection({ kind: "traffic", data: c });
    } catch (e) {
      if (req === latestDetailReq.current) showToast("get capture: " + e);
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

  const displayedWebhooks = pausedRows ? pausedRows.webhooks : visibleWebhooks;
  const displayedCaptures = pausedRows ? pausedRows.captures : visibleCaptures;

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

  const paletteActions = [
    { id: "webhooks", label: "Show ingress", glyph: "↘", run: () => changeTab("webhooks") },
    { id: "traffic", label: "Show traffic", glyph: "⇄", run: () => changeTab("traffic") },
    { id: "settings", label: "Open settings", glyph: "⚙", run: toggleSettings },
    { id: "transform", label: "New transform", glyph: "+", run: newScript },
    { id: "clear", label: "Clear filters", glyph: "×", run: () => { setSearch(""); setMethodFilter(""); setStatusFilter(""); setProject(""); setSessionFilter(0); } },
    ...(selection?.kind === "traffic" ? [{ id: "copy-url", label: "Copy selected URL", glyph: "↗", run: () => copyText(selection.data.url).then(() => showToast("URL copied")) }] : []),
    ...(selection?.kind === "webhook" ? [{ id: "replay", label: "Replay selected webhook", glyph: "↻", run: async () => {
      const target = (status && status.forward_url) || "";
      if (!target) {
        showToast("Set a default forward URL in Settings first");
        return;
      }
      try {
        const result = await api.replayWebhook(selection.data.project, selection.data.seq, target);
        showToast(`Replayed → HTTP ${result.status}`);
      } catch (e) {
        showToast("replay: " + e, 5000);
      }
    } }] : []),
  ];

  const followButton = html`<button class="follow-control ${follow ? "active" : ""}" onClick=${() => {
    if (follow) setPausedRows({ webhooks: visibleWebhooks, captures: visibleCaptures });
    else setPausedRows(null);
    setFollow((v) => !v);
    setQueuedCount(0);
  }}>
    <span class="live-dot ${follow ? "online" : ""}"></span>${follow ? "Following" : "Paused"}
  </button>`;

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
                counts=${{ webhooks: displayedWebhooks.length, traffic: displayedCaptures.length }}
                onSearch=${setSearch}
                project=${project}
                filtered=${activeTab === "webhooks" ? displayedWebhooks.length : displayedCaptures.length}
                followControl=${html`<span class="follow-cluster">
                  ${followButton}
                  ${queuedCount > 0 ? html`<button class="new-events-pill" onClick=${() => { setFollow(true); setPausedRows(null); setQueuedCount(0); }}>${queuedCount} new event${queuedCount === 1 ? "" : "s"}</button>` : null}
                </span>`}
              />
              <main class="workspace-main">
                <section class="event-stage">
                  ${activeTab === "webhooks"
                    ? html`<${WebhookList}
                        webhooks=${displayedWebhooks}
                        onSelect=${openWebhook}
                        selectedKey=${selection && selection.kind === "webhook"
                          ? `${selection.data.project}-${selection.data.seq}`
                          : null}
                      />`
                    : html`<${TrafficList}
                        captures=${displayedCaptures}
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
    ${paletteOpen ? html`<${CommandPalette} actions=${paletteActions} onClose=${() => setPaletteOpen(false)} />` : null}
  </div>`;
}

render(html`<${App} />`, document.getElementById("app"));
