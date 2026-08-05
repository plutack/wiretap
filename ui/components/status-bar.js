// StatusBar renders the app header: build version, store/relay/tunnel state and
// the list of projects the relay says this client is watching. It is a pure
// view over the polled Status() payload passed down from the root.
import { html } from "../vendor/preact/index.js";
import { IconButton } from "./ui.js";

function Dot({ ok }) {
  return html`<span
    class="h-2 w-2 rounded-full ${ok ? "bg-emerald-400 shadow-[0_0_6px] shadow-emerald-400/60" : "bg-neutral-600"}"
  ></span>`;
}

function Stat({ ok, label, value }) {
  return html`<span class="flex items-center gap-1.5 text-xs text-neutral-400">
    <${Dot} ok=${ok} />
    <span class="text-neutral-500">${label}</span>
    <b class="font-medium ${ok ? "text-neutral-200" : "text-neutral-500"}">${value}</b>
  </span>`;
}

export function StatusBar({ status, onRefresh }) {
  const s = status || {};
  const projects = s.connected_projects || [];
  const watching = projects.length
    ? projects.join(", ")
    : s.tunnel_running
      ? "—"
      : "tunnel down";

  return html`<header
    class="flex items-center gap-4 border-b border-neutral-800 bg-neutral-900/40 px-4 py-2.5"
  >
    <div class="flex items-center gap-2">
      <div class="grid h-6 w-6 place-items-center rounded-md bg-brand-500/15 text-brand-300">
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M3 10h4l2 5 3-10 2 5h3" />
        </svg>
      </div>
      <h1 class="text-sm font-semibold tracking-tight text-neutral-100">wiretap</h1>
      <span class="chip bg-neutral-800 text-neutral-400">v${s.version || "?"}</span>
    </div>

    <div class="flex items-center gap-4">
      <${Stat} ok=${!!s.store_open} label="store" value=${s.store_open ? "open" : "closed"} />
      ${s.relay_url
        ? html`<${Stat}
            ok=${!!s.tunnel_running}
            label="tunnel"
            value=${s.tunnel_running ? "live" : "idle"}
          />`
        : null}
      ${s.relay_url
        ? html`<span class="flex items-center gap-1.5 text-xs text-neutral-400">
            <span class="text-neutral-500">watching</span>
            <b class="max-w-[40ch] truncate font-mono text-brand-300">${watching}</b>
          </span>`
        : null}
    </div>

    <div class="ml-auto">
      <${IconButton} label="Refresh" onClick=${onRefresh}>
        <svg width="15" height="15" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
          <path d="M16 10a6 6 0 1 1-1.8-4.3M16 3v3h-3" />
        </svg>
      </>
    </div>
  </header>`;
}
