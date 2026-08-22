// BodyViewer inspects captured request/response bodies without assuming they are
// UTF-8. Auto follows Content-Type; explicit modes handle incorrect headers.
//
// Bodies are rendered through a window: only the first DISPLAY_LIMIT bytes are
// decoded and put into the DOM, regardless of payload size. Materializing a
// multi-megabyte body as DOM text cost 0.5–2s of blocked webview main thread
// per open (and piled up fatally under rapid row switching), while the full
// bytes remain available for Copy, Save, Image, and an explicit
// "show entire body" escalation.
import { html } from "../vendor/preact/index.js";
import { useEffect, useMemo, useState } from "../vendor/preact/index.js";
import { prettyBody, highlightJSON, escapeHTML } from "../lib/format.js";
import { copyText } from "../lib/clipboard.js";
import { Dropdown } from "./dropdown.js";

// How much of a body is decoded and rendered up front. Above this the viewer
// shows a slice plus an escalation button. 256 KiB keeps decode + escape +
// DOM work in the tens of milliseconds even when the payload is megabytes.
const DISPLAY_LIMIT = 256 * 1024;
// Pretty-print and JSON highlighting are only attempted below this size.
// Both are synchronous O(body) passes that build several multiples of the
// body size in strings and DOM.
const PRETTY_LIMIT = 1024 * 1024;
// Preview is refused outright above this size; Save still works.
const LARGE_BODY_LIMIT = 15 * 1024 * 1024;

function mediaType(contentType) {
  return String(contentType || "").split(";", 1)[0].trim().toLowerCase();
}

function autoMode(type) {
  if (/^image\/(png|jpe?g|gif|webp|svg\+xml)$/.test(type)) return "image";
  if (/json|javascript|xml|html|css|yaml|toml|graphql/.test(type)) return "pretty";
  if (type.startsWith("text/") || !type) return "text";
  return "hex";
}

function decodeBase64(encoded) {
  if (!encoded) return new Uint8Array();
  const raw = atob(encoded);
  const bytes = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i);
  return bytes;
}

// sliceBase64 cuts a byte-prefix of a base64 string. Only whole 4-char
// blocks decode to bytes, so the cut is aligned down to one.
function sliceBase64(encoded, maxBytes) {
  const chars = Math.floor(maxBytes / 3) * 4;
  return encoded.length <= chars ? encoded : encoded.slice(0, chars);
}

function bytesToText(bytes) {
  return new TextDecoder("utf-8", { fatal: false }).decode(bytes);
}

function hexDump(bytes) {
  const lines = [];
  for (let offset = 0; offset < bytes.length; offset += 16) {
    const chunk = bytes.slice(offset, offset + 16);
    const hex = Array.from(chunk, (b) => b.toString(16).padStart(2, "0")).join(" ");
    const ascii = Array.from(chunk, (b) => b >= 32 && b <= 126 ? String.fromCharCode(b) : ".").join("");
    lines.push(`${offset.toString(16).padStart(8, "0")}  ${hex.padEnd(47, " ")}  |${ascii}|`);
  }
  return lines.join("\n");
}

function fmtMB(n) {
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

export function BodyViewer({ body, bodyBase64, bodyLength, contentType, maxHeightClass = "max-h-72" }) {
  const [mode, setMode] = useState("auto");
  const [copyState, setCopyState] = useState("idle");
  const [showAll, setShowAll] = useState(false);
  const type = mediaType(contentType);
  const effectiveMode = mode === "auto" ? autoMode(type) : mode;

  const totalLength = bodyLength || (bodyBase64 ? Math.floor(bodyBase64.length * 3 / 4) : (body || "").length);
  const truncated = !showAll && totalLength > DISPLAY_LIMIT;

  // A new body resets the escalation so a big row never opens expanded.
  useEffect(() => {
    setShowAll(false);
  }, [bodyBase64, body]);

  // The display window: at most DISPLAY_LIMIT bytes are ever decoded for
  // rendering. Copy, Save, and Image decode the full payload on demand.
  const displayB64 = useMemo(
    () => (bodyBase64 && truncated ? sliceBase64(bodyBase64, DISPLAY_LIMIT) : bodyBase64 || ""),
    [bodyBase64, truncated],
  );
  const displayText = useMemo(
    () => (!bodyBase64 && body ? (truncated ? body.slice(0, DISPLAY_LIMIT) : body) : ""),
    [body, bodyBase64, truncated],
  );
  const bytes = useMemo(
    () => (displayB64 ? decodeBase64(displayB64) : new TextEncoder().encode(displayText || "")),
    [displayB64, displayText],
  );
  const text = useMemo(() => bytesToText(bytes), [bytes]);
  const isLarge = totalLength > LARGE_BODY_LIMIT;
  const imageURL = useMemo(() => {
    if (effectiveMode !== "image" || !totalLength || !/^image\//.test(type)) return "";
    // Images need the complete bytes, however large; they are the one mode
    // that cannot render from a prefix.
    const full = bodyBase64 ? decodeBase64(bodyBase64) : new TextEncoder().encode(body || "");
    if (!full.length) return "";
    return URL.createObjectURL(new Blob([full], { type: type || "application/octet-stream" }));
  }, [body, bodyBase64, effectiveMode, type, totalLength]);
  useEffect(() => () => { if (imageURL) URL.revokeObjectURL(imageURL); }, [imageURL]);

  if (!totalLength && !bytes.length) return html`<p class="body-empty">(empty)</p>`;

  // Heavy transforms are memoized and only attempted within the display
  // window; the poll loop re-renders this component every cycle for free.
  const oversizePretty = totalLength > PRETTY_LIMIT;
  const pretty = useMemo(() => {
    if (effectiveMode !== "pretty" || oversizePretty || truncated) return { text, isJSON: false };
    return prettyBody(text, contentType);
  }, [text, contentType, effectiveMode, oversizePretty, truncated]);
  const renderedText = pretty.isJSON ? pretty.text : text;
  const inner = useMemo(() => {
    if (pretty.isJSON) return highlightJSON(renderedText);
    return escapeHTML(effectiveMode === "hex" ? hexDump(bytes) : renderedText);
  }, [pretty, renderedText, effectiveMode, bytes]);
  // Stable object identity so Preact does not re-assign innerHTML when the
  // content is unchanged.
  const innerHTML = useMemo(() => ({ __html: inner }), [inner]);

  const fullText = () => (bodyBase64 ? bytesToText(decodeBase64(bodyBase64)) : (body || ""));
  const fullBytes = () => (bodyBase64 ? decodeBase64(bodyBase64) : new TextEncoder().encode(body || ""));
  const copy = async () => {
    try {
      await copyText(fullText());
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
    setTimeout(() => setCopyState("idle"), 1600);
  };
  const download = () => {
    const url = URL.createObjectURL(new Blob([fullBytes()], { type: type || "application/octet-stream" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `wiretap-body.${type.split("/")[1]?.split("+")[0] || "bin"}`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  };

  return html`<div class="body-viewer">
    <div class="body-viewer-toolbar">
      <span class="body-type">${type || "unknown type"}</span>
      <${Dropdown}
        value=${mode}
        options=${[
          { value: "auto", label: `Auto (${effectiveMode})` },
          { value: "pretty", label: "Pretty" },
          { value: "text", label: "Text" },
          { value: "image", label: "Image" },
          { value: "raw", label: "Raw" },
          { value: "hex", label: "Hex" },
        ]}
        onChange=${(event) => setMode(event.target.value)}
        aria-label="Body view mode"
        class="body-viewer-mode"
      />
      <button class="body-action" onClick=${download}>Save</button>
      ${effectiveMode !== "image" ? html`<button class="body-action" onClick=${copy}>${copyState === "copied" ? "Copied" : copyState === "failed" ? "Copy failed" : "Copy"}</button>` : null}
    </div>
    ${truncated
      ? html`<div class="body-large-warning">
          Showing the first ${Math.round(DISPLAY_LIMIT / 1024)} KB of ${fmtMB(totalLength)}.
          <button class="body-action" onClick=${() => setShowAll(true)}>Show entire body</button>
          (slower for large bodies) — Copy and Save always use the full payload.
        </div>`
      : oversizePretty && effectiveMode === "pretty"
        ? html`<div class="body-large-warning">Body is over 1 MB — pretty-print and highlighting are skipped for responsiveness. Use Text, Hex, or Save.</div>`
        : null}
    ${isLarge
      ? html`<div class="body-large-warning">This body is ${fmtMB(totalLength)}. Previewing it may use significant memory. Save the original to inspect it externally.</div>`
      : effectiveMode === "image" && imageURL
        ? html`<div class="body-image-wrap"><img class="body-image" src=${imageURL} alt="Captured ${type} body" /></div>`
        : html`<pre class="${maxHeightClass} body-code" dangerouslySetInnerHTML=${innerHTML}></pre>`}
  </div>`;
}

export const CodeBlock = BodyViewer;
