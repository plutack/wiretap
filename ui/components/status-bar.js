import { html } from "../vendor/preact/index.js";

function SystemPill({ online, label, value, optional = false }) {
  return html`<div class="system-pill ${optional ? "optional" : ""}">
    <span class="live-dot ${online ? "online" : ""}"></span>
    <span>${label}</span>
    <strong title=${value}>${value}</strong>
  </div>`;
}

export function StatusBar({ status, onRefresh, onOpenSettings, settingsActive }) {
  const s = status || {};
  const projects = s.connected_projects || [];
  const watching = projects.length
    ? projects.join(" · ")
    : s.tunnel_running
      ? "negotiating"
      : "offline";

  return html`<header class="topbar">
    <div class="brand-lockup">
      <div class="brand-mark" aria-hidden="true">
        <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
          <path d="M2 12h4l2.2-6 3.6 12 3-9 2.1 3H22" />
        </svg>
      </div>
      <div>
        <div class="brand-name">wiretap</div>
        <div class="brand-version">signal workbench · v${s.version || "dev"}</div>
      </div>
    </div>

    <div class="system-strip">
      <${SystemPill}
        online=${!!s.store_open}
        label="store"
        value=${s.store_open ? "recording" : "unavailable"}
      />
      ${s.relay_url
        ? html`<${SystemPill}
            online=${!!s.tunnel_running}
            label="relay"
            value=${s.tunnel_running ? "connected" : "idle"}
          />
          <${SystemPill}
            online=${!!s.tunnel_running && projects.length > 0}
            label="watching"
            value=${watching}
            optional=${true}
          />`
        : html`<div class="system-pill optional">
            <span class="live-dot"></span>
            <span>relay</span>
            <strong>not configured</strong>
          </div>`}
    </div>

    <div class="topbar-actions">
    <button
      class="refresh-button ${settingsActive ? "active" : ""}"
      title="Settings"
      aria-label="Settings"
      onClick=${onOpenSettings}
    >
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="3" />
        <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.01a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h.01a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v.01a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
      </svg>
    </button>
    <button class="refresh-button" title="Refresh all data" aria-label="Refresh all data" onClick=${onRefresh}>
      <svg width="15" height="15" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
        <path d="M16 10a6 6 0 1 1-1.8-4.3M16 3v3h-3" />
      </svg>
    </button>
    </div>
  </header>`;
}
