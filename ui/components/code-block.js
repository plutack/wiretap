// BodyViewer renders bounded previews. Syntax highlighting is reserved for
// small bodies; larger bodies use one text node and expand only on demand.
import { html } from "../vendor/preact/index.js";
import { useEffect, useMemo, useRef, useState } from "../vendor/preact/index.js";
import { prettyBody, highlightJSON } from "../lib/format.js";
import { copyText } from "../lib/clipboard.js";
import { Dropdown } from "./dropdown.js";

const HIGHLIGHT_LIMIT = 100 * 1024;
const PREVIEW_STEP = 256 * 1024;

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

export function BodyViewer({
  body,
  bodyBase64,
  bodyLength,
  truncated = false,
  contentType,
  loadBody,
  maxHeightClass = "max-h-72",
}) {
  const [mode, setMode] = useState("auto");
  const [copyState, setCopyState] = useState("idle");
  const [previewBase64, setPreviewBase64] = useState(bodyBase64 || "");
  const [previewTruncated, setPreviewTruncated] = useState(truncated);
  const [loadState, setLoadState] = useState("idle");
  const loadID = useRef(0);
  const type = mediaType(contentType);

  useEffect(() => {
    loadID.current += 1;
    setPreviewBase64(bodyBase64 || "");
    setPreviewTruncated(truncated);
    setLoadState("idle");
  }, [bodyBase64, truncated]);

  const bytes = useMemo(
    () => previewBase64 ? decodeBase64(previewBase64) : new TextEncoder().encode(body || ""),
    [body, previewBase64],
  );
  const text = useMemo(() => bytesToText(bytes), [bytes]);
  const selectedMode = mode === "auto" ? autoMode(type) : mode;
  const canHighlight = bytes.length <= HIGHLIGHT_LIMIT && !previewTruncated;
  const effectiveMode = selectedMode === "pretty" && !canHighlight ? "text" : selectedMode;
  const totalLength = bodyLength ?? bytes.length;

  const formatted = useMemo(() => {
    if (effectiveMode === "hex") return { text: hexDump(bytes), html: "" };
    if (effectiveMode === "pretty") {
      const pretty = prettyBody(text, contentType);
      return {
        text: pretty.isJSON ? pretty.text : text,
        html: pretty.isJSON ? highlightJSON(pretty.text) : "",
      };
    }
    return { text, html: "" };
  }, [bytes, contentType, effectiveMode, text]);

  const imageURL = useMemo(() => {
    if (effectiveMode !== "image" || previewTruncated || !bytes.length || !/^image\//.test(type)) return "";
    return URL.createObjectURL(new Blob([bytes], { type: type || "application/octet-stream" }));
  }, [bytes, effectiveMode, previewTruncated, type]);
  useEffect(() => () => { if (imageURL) URL.revokeObjectURL(imageURL); }, [imageURL]);

  if (!bytes.length && !previewTruncated) return html`<p class="body-empty">(empty)</p>`;

  const fetchBody = async (limit, purpose) => {
    if (!loadBody) return null;
    const id = ++loadID.current;
    setLoadState(purpose);
    try {
      const result = await loadBody(limit);
      if (id !== loadID.current) return null;
      if (purpose === "preview") {
        setPreviewBase64(result.body_base64 || "");
        setPreviewTruncated(Boolean(result.truncated));
      }
      setLoadState("idle");
      return result;
    } catch {
      if (id === loadID.current) setLoadState("failed");
      return null;
    }
  };

  const showMore = async () => {
    const nextLimit = Math.min(totalLength, Math.max(PREVIEW_STEP, bytes.length * 2));
    await fetchBody(nextLimit, "preview");
  };
  const copy = async () => {
    try {
      let value = formatted.text;
      if (previewTruncated) {
        const result = await fetchBody(0, "copy");
        if (!result) return;
        value = bytesToText(decodeBase64(result.body_base64));
      }
      await copyText(value);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
    setTimeout(() => setCopyState("idle"), 1600);
  };
  const download = async () => {
    const result = previewTruncated ? await fetchBody(0, "save") : null;
    if (previewTruncated && !result) return;
    const downloadBytes = result ? decodeBase64(result.body_base64) : bytes;
    const url = URL.createObjectURL(new Blob([downloadBytes], { type: type || "application/octet-stream" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `wiretap-body.${type.split("/")[1]?.split("+")[0] || "bin"}`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  };

  const modeLabel = mode === "auto" && selectedMode !== effectiveMode
    ? `Auto (${effectiveMode} preview)`
    : `Auto (${effectiveMode})`;

  return html`<div class="body-viewer">
    <div class="body-viewer-toolbar">
      <span class="body-type">${type || "unknown type"}</span>
      <${Dropdown}
        value=${mode}
        options=${[
          { value: "auto", label: modeLabel },
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
      <button class="body-action" disabled=${loadState !== "idle"} onClick=${download}>
        ${loadState === "save" ? "Loading…" : "Save"}
      </button>
      ${effectiveMode !== "image" ? html`<button class="body-action" disabled=${loadState !== "idle"} onClick=${copy}>
        ${loadState === "copy" ? "Loading…" : copyState === "copied" ? "Copied" : copyState === "failed" ? "Copy failed" : previewTruncated ? "Copy all" : "Copy"}
      </button>` : null}
    </div>
    ${previewTruncated ? html`<div class="body-large-warning">
      Showing ${Math.round(bytes.length / 1024)} KB of ${Math.round(totalLength / 1024)} KB as plain text to keep the inspector responsive.
      <button class="body-action" disabled=${loadState !== "idle"} onClick=${showMore}>
        ${loadState === "preview" ? "Loading…" : "Show more"}
      </button>
      ${loadState === "failed" ? html`<span>Could not load more.</span>` : null}
    </div>` : selectedMode === "pretty" && !canHighlight ? html`<div class="body-large-warning">
      Syntax highlighting is disabled above ${Math.round(HIGHLIGHT_LIMIT / 1024)} KB; displaying one plain-text node.
    </div>` : null}
    ${effectiveMode === "image" && previewTruncated
      ? html`<p class="body-empty">The image exceeds the automatic preview limit. Save it to inspect the complete file.</p>`
      : effectiveMode === "image" && imageURL
        ? html`<div class="body-image-wrap"><img class="body-image" src=${imageURL} alt="Captured ${type} body" /></div>`
      : formatted.html
        ? html`<pre class="${maxHeightClass} body-code" dangerouslySetInnerHTML=${{ __html: formatted.html }}></pre>`
        : html`<pre class="${maxHeightClass} body-code">${formatted.text}</pre>`}
  </div>`;
}

export const CodeBlock = BodyViewer;
