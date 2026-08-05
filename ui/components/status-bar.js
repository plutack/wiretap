import { html } from "../vendor/preact/index.js";

function SystemPill({ online, label, value, optional = false }) {
  return html`<div class="system-pill ${optional ? "optional" : ""}">
    <span class="live-dot ${online ? "online" : ""}"></span>
    <span>${label}</span>
    <strong title=${value}>${value}</strong>
  </div>`;
}

export function StatusBar({ status, onRefresh }) {
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

    <button class="refresh-button" title="Refresh all data" aria-label="Refresh all data" onClick=${onRefresh}>
      <svg width="15" height="15" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
        <path d="M16 10a6 6 0 1 1-1.8-4.3M16 3v3h-3" />
      </svg>
    </button>
  </header>`;
}
