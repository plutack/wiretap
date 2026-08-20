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
      <col style="width: 170px" />
      <col style="width: 112px" />
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
      ${captures.map((capture, i) => {
        const destination = splitURL(capture.url);
        // Dim the host on consecutive rows to the same destination so unique
        // requests pop out of a burst of same-API traffic.
        const prev = i > 0 ? splitURL(captures[i - 1].url) : null;
        const repeatHost = prev !== null && prev.host === destination.host;
        return html`<tr
          key=${capture.id}
          class="signal-row ${capture.id === selectedId ? "active" : ""}"
          onClick=${() => onSelect(capture.id)}
        >
          <td><${MethodBadge} method=${capture.method} /></td>
          <td>
            <span class="signal-primary" title=${capture.url}>${destination.path}</span>
            <span class="signal-secondary ${repeatHost ? "repeat" : ""}">${destination.host} · capture ${capture.id}</span>
          </td>
          <td><${StatusBadge} status=${capture.status} /></td>
          <td class="transfer-cell">
            <span class="transfer-values">
              <span class="transfer-up" title="Request upload">↑ ${fmtBytes(capture.req_body_len) || "0 B"}</span>
              <span class="transfer-separator">·</span>
              <span class="transfer-down" title="Response download">↓ ${fmtBytes(capture.resp_body_len) || "0 B"}</span>
            </span>
          </td>
          <td class="mono-dim time-cell">${fmtTime(capture.at)}</td>
        </tr>`;
      })}
    </tbody>
  </table>`;
}
