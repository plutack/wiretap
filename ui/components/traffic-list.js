import { html } from "../vendor/preact/index.js";
import { MethodBadge, StatusBadge } from "./badges.js";
import { fmtTime, fmtBytes } from "../lib/format.js";

function splitURL(rawURL) {
  try {
    const url = new URL(rawURL);
    return { host: url.host, path: url.pathname + url.search };
  } catch {
    return { host: "captured request", path: rawURL || "/" };
  }
}

export function TrafficList({ captures, onSelect, selectedId }) {
  if (captures.length === 0) {
    return html`<div class="empty-workspace">
      <div class="empty-card">
        <div class="empty-radar">⇄</div>
        <h2>No local traffic in the stream</h2>
        <p>
          Start <code>wiretap intercept start</code>, then make requests from the spawned shell to record exchanges.
        </p>
      </div>
    </div>`;
  }

  return html`<table class="signal-table">
    <colgroup>
      <col style="width: 82px" />
      <col />
      <col style="width: 78px" />
      <col style="width: 112px" />
      <col style="width: 76px" />
    </colgroup>
    <thead>
      <tr>
        <th>Method</th>
        <th>Destination</th>
        <th>Status</th>
        <th>Transfer</th>
        <th>Seen</th>
      </tr>
    </thead>
    <tbody>
      ${captures.map((capture) => {
        const destination = splitURL(capture.url);
        return html`<tr
          key=${capture.id}
          class="signal-row ${capture.id === selectedId ? "active" : ""}"
          onClick=${() => onSelect(capture.id)}
        >
          <td><${MethodBadge} method=${capture.method} /></td>
          <td>
            <span class="signal-primary" title=${capture.url}>${destination.path}</span>
            <span class="signal-secondary">${destination.host} · capture ${capture.id}</span>
          </td>
          <td><${StatusBadge} status=${capture.status} /></td>
          <td class="mono-dim">
            ↑ ${fmtBytes(capture.req_body_len) || "0 B"} · ↓ ${fmtBytes(capture.resp_body_len) || "0 B"}
          </td>
          <td class="mono-dim">${fmtTime(capture.at)}</td>
        </tr>`;
      })}
    </tbody>
  </table>`;
}
