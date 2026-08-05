// WebhookDetail renders the full webhook (headers, body, replay form). It's a
// controlled component: the parent fetches the webhook and passes it down.
import { html } from "../vendor/preact/index.js";
import { useState } from "../vendor/preact/index.js";
import { MethodBadge, HeaderTable } from "./badges.js";
import { CodeBlock } from "./code-block.js";
import { DetailPane, DetailBody, BodySection } from "./detail-pane.js";
import { Button, Input } from "./ui.js";
import { fmtBytes } from "../lib/format.js";

function headerValue(headers, name) {
  const entry = Object.entries(headers || {}).find(
    ([k]) => k.toLowerCase() === name.toLowerCase(),
  );
  if (!entry) return "";
  const v = entry[1];
  return Array.isArray(v) ? v.join(", ") : String(v);
}

export function WebhookDetail({ webhook, onReplay, onClose }) {
  const [targetURL, setTargetURL] = useState("");
  const [replayState, setReplayState] = useState(null); // {status, error}

  const handleReplay = async () => {
    if (!targetURL.trim()) return;
    setReplayState({ status: "sending" });
    try {
      const result = await onReplay(webhook.project, webhook.seq, targetURL);
      setReplayState({ status: result.status });
    } catch (e) {
      setReplayState({ error: String(e) });
    }
  };

  const ct = headerValue(webhook.headers, "content-type");

  return html`<${DetailPane}
    title=${html`${webhook.project}/${webhook.seq}`}
    onClose=${onClose}
  >
    <${DetailBody}>
      <div class="exchange-hero">
        <${MethodBadge} method=${webhook.method} />
        <span class="exchange-url mt-0">${webhook.path}</span>
      </div>

      <section class="inspector-section">
        <div class="inspector-label">Headers</div>
        <${HeaderTable} headers=${webhook.headers} />
      </section>

      <${BodySection} title="Body" len=${fmtBytes(webhook.body_len)}>
        <${CodeBlock} body=${webhook.body} contentType=${ct} />
      </>

      <section class="inspector-section border-t border-neutral-800 pt-4">
        <div class="inspector-label">Replay to local target</div>
        <div class="flex gap-2">
          <${Input}
            type="text"
            placeholder="http://127.0.0.1:8080/hook"
            value=${targetURL}
            onInput=${(e) => setTargetURL(e.target.value)}
            class="font-mono"
          />
          <${Button} variant="primary" onClick=${handleReplay} class="flex-shrink-0">
            Replay
          </>
        </div>
        ${replayState &&
        html`<p
          class="mt-2 text-xs ${replayState.error
            ? "text-rose-400"
            : replayState.status === "sending"
              ? "text-neutral-400"
              : "text-emerald-400"}"
        >
          ${replayState.error
            ? `error: ${replayState.error}`
            : replayState.status === "sending"
              ? "sending…"
              : `replayed → HTTP ${replayState.status}`}
        </p>`}
      </section>
    </>
  </>`;
}
