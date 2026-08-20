// BodyViewer inspects captured request/response bodies without assuming they are
// UTF-8. Auto follows Content-Type; explicit modes handle incorrect headers.
import { html } from "../vendor/preact/index.js";
import { useEffect, useMemo, useState } from "../vendor/preact/index.js";
import { prettyBody, highlightJSON, escapeHTML } from "../lib/format.js";
import { copyText } from "../lib/clipboard.js";
import { Dropdown } from "./dropdown.js";

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

export function BodyViewer({ body, bodyBase64, bodyLength, contentType, maxHeightClass = "max-h-72" }) {
  const [mode, setMode] = useState("auto");
  const [copyState, setCopyState] = useState("idle");
  const type = mediaType(contentType);
  const bytes = useMemo(() => bodyBase64 ? decodeBase64(bodyBase64) : new TextEncoder().encode(body || ""), [body, bodyBase64]);
  const effectiveMode = mode === "auto" ? autoMode(type) : mode;
  const text = useMemo(() => bytesToText(bytes), [bytes]);
  const isLarge = (bodyLength || bytes.length) > LARGE_BODY_LIMIT;
  const imageURL = useMemo(() => {
    if (effectiveMode !== "image" || !bytes.length || !/^image\//.test(type)) return "";
    return URL.createObjectURL(new Blob([bytes], { type: type || "application/octet-stream" }));
  }, [bytes, effectiveMode, type]);
  useEffect(() => () => { if (imageURL) URL.revokeObjectURL(imageURL); }, [imageURL]);

  if (!bytes.length) return html`<p class="body-empty">(empty)</p>`;

  const pretty = prettyBody(text, contentType);
  const renderedText = effectiveMode === "pretty" && pretty.isJSON ? pretty.text : text;
  const inner = effectiveMode === "pretty" && pretty.isJSON
    ? highlightJSON(renderedText)
    : escapeHTML(effectiveMode === "hex" ? hexDump(bytes) : renderedText);
  const copy = async () => {
    try { await copyText(renderedText); setCopyState("copied"); }
    catch { setCopyState("failed"); }
    setTimeout(() => setCopyState("idle"), 1600);
  };
  const download = () => {
    const url = URL.createObjectURL(new Blob([bytes], { type: type || "application/octet-stream" }));
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
    ${isLarge
      ? html`<div class="body-large-warning">This body is ${Math.round((bodyLength || bytes.length) / 1024 / 1024)} MB. Previewing it may use significant memory. Save the original to inspect it externally.</div>`
      : effectiveMode === "image" && imageURL
        ? html`<div class="body-image-wrap"><img class="body-image" src=${imageURL} alt="Captured ${type} body" /></div>`
        : html`<pre class="${maxHeightClass} body-code" dangerouslySetInnerHTML=${{ __html: inner }}></pre>`}
  </div>`;
}

export const CodeBlock = BodyViewer;
