// TrafficDetail renders a captured HTTP exchange with full request/response
// headers and bodies. The parent fetches the detail via GetCapture (ListCaptures
// only returns summaries) and passes the populated CaptureView down.
import { html } from "../vendor/preact/index.js";
import { MethodBadge, StatusBadge, HeaderTable } from "./badges.js";
import { CodeBlock } from "./code-block.js";
import { DetailPane, DetailBody, BodySection } from "./detail-pane.js";
import { ExportSnippet } from "./export-snippet.js";
import { fmtBytes, fmtTime } from "../lib/format.js";

// Pull a header value case-insensitively (header maps are {name:[values]}).
function headerValue(headers, name) {
  const entry = Object.entries(headers || {}).find(
    ([k]) => k.toLowerCase() === name.toLowerCase(),
  );
  if (!entry) return "";
  const v = entry[1];
  return Array.isArray(v) ? v.join(", ") : String(v);
}

export function TrafficDetail({ capture, onExport, onClose }) {
  const reqCT = headerValue(capture.req_headers, "content-type");
  const respCT = headerValue(capture.resp_headers, "content-type");

  return html`<${DetailPane}
    title=${html`capture/${capture.id}`}
    onClose=${onClose}
  >
    <${DetailBody}>
      <div class="exchange-hero">
        <div class="flex flex-wrap items-center gap-2">
          <${MethodBadge} method=${capture.method} />
          <${StatusBadge} status=${capture.status} />
          <span class="ml-auto text-xs text-neutral-500">${fmtTime(capture.at)}</span>
        </div>
        <p class="exchange-url">${capture.url}</p>
      </div>

      <section class="inspector-section">
        <div class="inspector-label">Request headers</div>
        <${HeaderTable} headers=${capture.req_headers} />
      </section>

      <${BodySection}
        title="Request body"
        len=${fmtBytes(capture.req_body_len)}
      >
        <${CodeBlock}
                  body=${capture.req_body}
                  bodyBase64=${capture.req_body_base64}
                  bodyLength=${capture.req_body_len}
                  contentType=${reqCT}
                />
      </>

      <section class="inspector-section">
        <div class="inspector-label">Response headers</div>
        <${HeaderTable} headers=${capture.resp_headers} />
      </section>

      <${BodySection}
        title="Response body"
        len=${fmtBytes(capture.resp_body_len)}
      >
        <${CodeBlock}
                  body=${capture.resp_body}
                  bodyBase64=${capture.resp_body_base64}
                  bodyLength=${capture.resp_body_len}
                  contentType=${respCT}
                />
      </>

      ${onExport
        ? html`<${ExportSnippet}
            exportKey=${`capture-${capture.id}`}
            convert=${onExport}
          />`
        : null}
    </>
  </>`;
}
