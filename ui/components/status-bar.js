// StatusBar renders the app header: build version, store/relay/tunnel state and
// the list of projects the relay says this client is watching. It is a pure
// view over the polled Status() payload passed down from the root.
import { html } from "../vendor/preact/index.js";

export function StatusBar({ status, onRefresh }) {
  const s = status || {};
  const projects = s.connected_projects || [];
  return html`<header
    class="flex items-center gap-4 border-b border-neutral-800 px-4 py-2"
  >
    <h1 class="text-base font-semibold tracking-tight text-neutral-100">
      wiretap
    </h1>
    <div class="flex items-center gap-3 text-xs text-neutral-400">
      <span class="rounded bg-neutral-800 px-1.5 py-0.5 font-mono"
        >v${s.version || "?"}</span
      >
      ${s.store_open &&
      html`<span>store: <b class="text-neutral-200">open</b></span>`}
      ${s.relay_url &&
      html`<span class="truncate max-w-[40ch]">relay: ${s.relay_url}</span>`}
      ${s.relay_url &&
      html`<span
        >tunnel:${" "}
        ${s.tunnel_running
          ? html`<b class="text-emerald-400">live</b>`
          : html`<b class="text-neutral-500">idle</b>`}</span
      >`}
      ${s.relay_url &&
      html`<span class="truncate max-w-[60ch]"
        >watching:${" "}
        ${projects.length
          ? projects.map(
              (p, i) =>
                html`${i > 0 ? ", " : ""}<b class="text-sky-300 font-mono"
                    >${p}</b
                  >`,
            )
          : s.tunnel_running
            ? html`<b class="text-neutral-500">—</b>`
            : html`<b class="text-neutral-500">tunnel down</b>`}</span
      >`}
    </div>
    <div class="ml-auto flex items-center gap-2">
      <button
        onClick=${onRefresh}
        class="rounded border border-neutral-700 px-2 py-1 text-xs hover:bg-neutral-800"
      >
        Refresh
      </button>
    </div>
  </header>`;
}
