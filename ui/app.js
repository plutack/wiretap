// wiretap GUI frontend. Calls the Wails-bound methods in
// ../wailsjs/go/gui/Bindings.js (auto-generated; do not edit). Each bound
// method returns a Promise that resolves with the JSON view from internal/gui.
import {
  GetWebhook,
  ListCaptures,
  ListWebhooks,
  ReplayWebhook,
  Status,
} from "./wailsjs/go/gui/Bindings.js";

const els = {
  statusVersion: document.getElementById("status-version"),
  statusStore: document.getElementById("status-store"),
  statusRelay: document.getElementById("status-relay"),
  statusTunnel: document.getElementById("status-tunnel"),
  statusWatching: document.getElementById("status-watching"),
  refresh: document.getElementById("refresh"),
  webhooksTable: document.getElementById("webhooks-table"),
  trafficTable: document.getElementById("traffic-table"),
  webhooksBody: document.getElementById("webhooks-body"),
  trafficBody: document.getElementById("traffic-body"),
  empty: document.getElementById("empty"),
  detail: document.getElementById("detail"),
  detailTitle: document.getElementById("detail-title"),
  detailBody: document.getElementById("detail-body"),
  detailClose: document.getElementById("detail-close"),
  toast: document.getElementById("toast"),
};

let activeTab = "webhooks";

// --- helpers -------------------------------------------------------------

function esc(s) {
  return String(s ?? "").replace(/[&<>"]/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;",
  }[c]));
}

function fmtTime(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString();
}

function toast(msg, ms = 3000) {
  els.toast.textContent = msg;
  els.toast.classList.remove("hidden");
  clearTimeout(toast._t);
  toast._t = setTimeout(() => els.toast.classList.add("hidden"), ms);
}

function methodBadge(method) {
  const colors = {
    GET: "bg-emerald-900/60 text-emerald-300",
    POST: "bg-sky-900/60 text-sky-300",
    PUT: "bg-amber-900/60 text-amber-300",
    PATCH: "bg-amber-900/60 text-amber-300",
    DELETE: "bg-rose-900/60 text-rose-300",
  };
  const cls = colors[(method || "").toUpperCase()] || "bg-neutral-800 text-neutral-300";
  return `<span class="rounded px-1.5 py-0.5 font-mono text-[11px] ${cls}">${esc(method || "")}</span>`;
}

function statusBadge(status) {
  if (!status) return `<span class="text-neutral-600">—</span>`;
  const cls = status < 300 ? "text-emerald-400" : status < 400 ? "text-sky-400" : status < 500 ? "text-amber-400" : "text-rose-400";
  return `<span class="font-mono ${cls}">${status}</span>`;
}

function headersToTable(headers) {
  const entries = Object.entries(headers || {});
  if (entries.length === 0) return `<p class="text-neutral-600">(no headers)</p>`;
  return `<table class="w-full text-xs"><tbody>${entries
    .map(([k, vs]) => `<tr><td class="align-top pr-3 font-mono text-neutral-400">${esc(k)}</td><td class="font-mono break-all">${esc(vs.join(", "))}</td></tr>`)
    .join("")}</tbody></table>`;
}

// --- status --------------------------------------------------------------

async function refreshStatus() {
  try {
    const s = await Status();
    els.statusVersion.textContent = "v" + s.version;
    els.statusStore.classList.toggle("hidden", !s.store_open);
    if (s.relay_url) {
      els.statusRelay.classList.remove("hidden");
      els.statusRelay.textContent = "relay: " + s.relay_url;
    } else {
      els.statusRelay.classList.add("hidden");
    }
    if (s.relay_url) {
      els.statusTunnel.classList.remove("hidden");
      els.statusTunnel.innerHTML = "tunnel: " + (s.tunnel_running
        ? '<b class="text-emerald-400">live</b>'
        : '<b class="text-neutral-500">idle</b>');
    } else {
      els.statusTunnel.classList.add("hidden");
    }
    // Connected projects = the list the relay says this client owns, given to
    // us by the tunnel's OnConnect. Hidden when relay isn't configured or the
    // tunnel has never connected; shows "—" while the tunnel is down so the
    // user can tell "no projects yet" apart from "tunnel isn't up".
    if (s.relay_url) {
      els.statusWatching.classList.remove("hidden");
      const ps = s.connected_projects || [];
      els.statusWatching.innerHTML = "watching: " + (ps.length
        ? '<b class="text-sky-300 font-mono">' + ps.map(esc).join("</b>, <b class=\"text-sky-300 font-mono\">") + "</b>"
        : (s.tunnel_running ? "<b class=\"text-neutral-500\">—</b>" : "<b class=\"text-neutral-500\">tunnel down</b>"));
    } else {
      els.statusWatching.classList.add("hidden");
    }
  } catch (e) {
    toast("status: " + e);
  }
}

// --- webhooks ------------------------------------------------------------

async function refreshWebhooks() {
  let rows = [];
  try {
    rows = await ListWebhooks("");
  } catch (e) {
    toast("list webhooks: " + e);
    return;
  }
  els.webhooksBody.innerHTML = rows
    .map((w) => `<tr class="cursor-pointer hover:bg-neutral-900" data-project="${esc(w.project)}" data-seq="${w.seq}">
      <td class="px-3 py-1.5 font-mono text-sky-300">${esc(w.project)}</td>
      <td class="px-3 py-1.5 font-mono text-neutral-400">${w.seq}</td>
      <td class="px-3 py-1.5">${methodBadge(w.method)}</td>
      <td class="px-3 py-1.5 font-mono text-neutral-300">${esc(w.path)}</td>
      <td class="px-3 py-1.5 text-neutral-500">${esc(fmtTime(w.received_at))}</td>
      <td class="px-3 py-1.5 text-right font-mono text-neutral-500">${w.body_len}b</td>
    </tr>`)
    .join("");
  els.webhooksBody.querySelectorAll("tr").forEach((tr) => {
    tr.addEventListener("click", () => openWebhook(tr.dataset.project, Number(tr.dataset.seq)));
  });
  toggleEmpty(rows.length);
}

async function openWebhook(project, seq) {
  let w;
  try {
    w = await GetWebhook(project, seq);
  } catch (e) {
    toast("get webhook: " + e);
    return;
  }
  els.detailTitle.textContent = `Webhook ${project}/${seq}`;
  els.detailBody.innerHTML = `
    <div class="mb-3 flex items-center gap-2">
      ${methodBadge(w.method)}
      <span class="font-mono text-neutral-300">${esc(w.path)}</span>
    </div>
    <section class="mb-4">
      <h3 class="mb-1 text-xs uppercase text-neutral-500">Headers</h3>
      ${headersToTable(w.headers)}
    </section>
    <section class="mb-4">
      <h3 class="mb-1 text-xs uppercase text-neutral-500">Body (${w.body_len}b)</h3>
      <pre class="max-h-48 overflow-auto rounded bg-neutral-950 p-2 text-xs font-mono break-all whitespace-pre-wrap">${esc(w.body)}</pre>
    </section>
    <section class="mt-4 border-t border-neutral-800 pt-3">
      <h3 class="mb-2 text-xs uppercase text-neutral-500">Replay to local target</h3>
      <div class="flex gap-2">
        <input id="replay-url" type="text" placeholder="http://127.0.0.1:8080/hook"
          class="min-w-0 flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1 text-sm font-mono" />
        <button id="replay-go" class="rounded bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500">Replay</button>
      </div>
      <p id="replay-result" class="mt-2 text-xs"></p>
    </section>`;
  els.detail.classList.remove("hidden");
  els.detail.classList.add("flex");
  document.getElementById("replay-go").addEventListener("click", () =>
    doReplay(project, seq, document.getElementById("replay-url").value));
}

async function doReplay(project, seq, targetURL) {
  const out = document.getElementById("replay-result");
  out.textContent = "sending…";
  out.className = "mt-2 text-xs text-neutral-400";
  try {
    const r = await ReplayWebhook(project, seq, targetURL);
    out.textContent = `replayed → HTTP ${r.status}`;
    out.className = "mt-2 text-xs text-emerald-400";
    toast(`Replayed ${project}/${seq} → ${r.status}`);
  } catch (e) {
    out.textContent = "error: " + e;
    out.className = "mt-2 text-xs text-rose-400";
    toast("replay failed: " + e);
  }
}

// --- traffic -------------------------------------------------------------

async function refreshTraffic() {
  let rows = [];
  try {
    rows = await ListCaptures();
  } catch (e) {
    toast("list captures: " + e);
    return;
  }
  els.trafficBody.innerHTML = rows
    .map((c) => `<tr class="cursor-pointer hover:bg-neutral-900" data-id="${c.id}">
      <td class="px-3 py-1.5 font-mono text-neutral-500">${c.id}</td>
      <td class="px-3 py-1.5">${methodBadge(c.method)}</td>
      <td class="px-3 py-1.5 font-mono text-neutral-300 break-all">${esc(c.url)}</td>
      <td class="px-3 py-1.5">${statusBadge(c.status)}</td>
      <td class="px-3 py-1.5 text-neutral-500">${esc(fmtTime(c.at))}</td>
      <td class="px-3 py-1.5 text-right font-mono text-neutral-500">${c.req_body_len} / ${c.resp_body_len}</td>
    </tr>`)
    .join("");
  els.trafficBody.querySelectorAll("tr").forEach((tr) =>
    tr.addEventListener("click", () => openCapture(Number(tr.dataset.id), rows)));
  toggleEmpty(rows.length);
}

function openCapture(id, rows) {
  const c = rows.find((r) => r.id === id);
  if (!c) return;
  els.detailTitle.textContent = `Capture #${c.id}`;
  els.detailBody.innerHTML = `
    <div class="mb-3 flex items-center gap-2">
      ${methodBadge(c.method)}
      <span class="font-mono text-neutral-300 break-all">${esc(c.url)}</span>
      ${statusBadge(c.status)}
    </div>
    <p class="text-xs text-neutral-500">
      Captures are returned as summaries by ListCaptures; the detail payload
      (full req/resp headers + bodies) is rendered from the summary view in this
      MVP. Byte counts: request ${c.req_body_len}b, response ${c.resp_body_len}b.
    </p>`;
  els.detail.classList.remove("hidden");
  els.detail.classList.add("flex");
}

// --- tabs / wiring -------------------------------------------------------

function toggleEmpty(n) {
  els.empty.classList.toggle("hidden", n !== 0);
}

function setTab(tab) {
  activeTab = tab;
  for (const btn of document.querySelectorAll(".tab-btn")) {
    const on = btn.dataset.tab === tab;
    btn.classList.toggle("border-sky-500", on);
    btn.classList.toggle("text-sky-400", on);
    btn.classList.toggle("border-transparent", !on);
    btn.classList.toggle("text-neutral-400", !on);
  }
  els.webhooksTable.classList.toggle("hidden", tab !== "webhooks");
  els.trafficTable.classList.toggle("hidden", tab !== "traffic");
  void (tab === "webhooks" ? refreshWebhooks() : refreshTraffic());
}

document.querySelectorAll(".tab-btn").forEach((btn) =>
  btn.addEventListener("click", () => setTab(btn.dataset.tab)));

els.detailClose.addEventListener("click", () => {
  els.detail.classList.add("hidden");
  els.detail.classList.remove("flex");
});

els.refresh.addEventListener("click", () => {
  refreshStatus();
  void (activeTab === "webhooks" ? refreshWebhooks() : refreshTraffic());
});

// Initial render + light polling (2s) so new captures/webhooks appear.
setTab("webhooks");
refreshStatus();
setInterval(refreshStatus, 5000);
setInterval(() => void (activeTab === "webhooks" ? refreshWebhooks() : refreshTraffic()), 2000);