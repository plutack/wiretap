import { html } from "../vendor/preact/index.js";
import { MethodBadge } from "./badges.js";
import { fmtTime, fmtBytes } from "../lib/format.js";

export function WebhookList({ webhooks, onSelect, selectedKey }) {
  if (webhooks.length === 0) {
    return html`<div class="empty-workspace">
      <div class="empty-card">
        <div class="empty-radar">⌁</div>
        <h2>Waiting for an inbound signal</h2>
        <p>
          Send a request to a claimed relay path. Offline deliveries will surface here when the tunnel reconnects.
        </p>
      </div>
    </div>`;
  }

  return html`<table class="signal-table">
    <colgroup>
      <col style="width: 82px" />
      <col style="width: 118px" />
      <col />
      <col style="width: 82px" />
      <col style="width: 76px" />
    </colgroup>
    <thead>
      <tr>
        <th>Method</th>
        <th>Source</th>
        <th>Route</th>
        <th>Payload</th>
        <th>Seen</th>
      </tr>
    </thead>
    <tbody>
      ${webhooks.map((webhook) => {
        const key = `${webhook.project}-${webhook.seq}`;
        return html`<tr
          key=${key}
          class="signal-row ${key === selectedKey ? "active" : ""}"
          onClick=${() => onSelect(webhook.project, webhook.seq)}
        >
          <td><${MethodBadge} method=${webhook.method} /></td>
          <td>
            <span class="signal-primary">${webhook.project}</span>
            <span class="signal-secondary">sequence ${webhook.seq}</span>
          </td>
          <td>
            <span class="signal-primary" title=${webhook.path}>${webhook.path || "/"}</span>
            <span class="signal-secondary">relay ingress</span>
          </td>
          <td class="mono-dim">${fmtBytes(webhook.body_len) || "empty"}</td>
          <td class="mono-dim">${fmtTime(webhook.received_at)}</td>
        </tr>`;
      })}
    </tbody>
  </table>`;
}
