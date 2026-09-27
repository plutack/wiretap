// Shared formatting helpers used across all components.

/**
 * Return the chip classes for a HTTP method badge. Colors are tuned so every
 * method is legible on the dark background (the previous palette left some
 * badges barely visible).
 * @param {string} method
 * @returns {string}
 */
export function methodBadgeClass(method) {
  const map = {
    GET: "bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/25",
    POST: "bg-brand-500/15 text-brand-300 ring-1 ring-brand-500/25",
    PUT: "bg-amber-500/15 text-amber-300 ring-1 ring-amber-500/25",
    PATCH: "bg-amber-500/15 text-amber-300 ring-1 ring-amber-500/25",
    DELETE: "bg-rose-500/15 text-rose-300 ring-1 ring-rose-500/25",
    HEAD: "bg-neutral-500/15 text-neutral-300 ring-1 ring-neutral-500/25",
    OPTIONS: "bg-neutral-500/15 text-neutral-300 ring-1 ring-neutral-500/25",
  };
  return (
    map[(method || "").toUpperCase()] ||
    "bg-neutral-700/40 text-neutral-300 ring-1 ring-neutral-600/40"
  );
}

/**
 * Return the Tailwind text-color class for an HTTP status code.
 * @param {number|undefined} status
 * @returns {string}
 */
export function statusColorClass(status) {
  if (!status) return "text-neutral-600";
  if (status < 300) return "text-emerald-400";
  if (status < 400) return "text-sky-400";
  if (status < 500) return "text-amber-400";
  return "text-rose-400";
}

/**
 * Format an ISO timestamp for display (locale time only).
 * @param {string} iso
 * @returns {string}
 */
export function fmtTime(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString();
}

/**
 * Return a human-readable byte count string.
 * @param {number} n
 * @returns {string}
 */
export function fmtBytes(n) {
  if (n == null) return "";
  if (n < 1024) return n + " B";
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
  return (n / (1024 * 1024)).toFixed(1) + " MB";
}

/**
 * Attempt to pretty-print a body as JSON. Returns {text, isJSON}: when the body
 * parses as JSON we return the 2-space-indented form; otherwise the original
 * string is returned untouched so non-JSON payloads are shown verbatim.
 * @param {string} body
 * @param {string} [contentType] optional content-type hint
 * @returns {{text: string, isJSON: boolean}}
 */
export function prettyBody(body, contentType) {
  const raw = body || "";
  if (!raw.trim()) return { text: raw, isJSON: false };
  // Only try JSON when it looks like JSON or the content-type says so — avoids
  // throwing on every plain-text body.
  const looksJSON = /^[\s]*[[{]/.test(raw) || /json/i.test(contentType || "");
  if (!looksJSON) return { text: raw, isJSON: false };
  try {
    return { text: JSON.stringify(JSON.parse(raw), null, 2), isJSON: true };
  } catch {
    return { text: raw, isJSON: false };
  }
}

/**
 * Escape HTML-special characters so a string is safe to inject as innerHTML.
 * @param {string} s
 * @returns {string}
 */
export function escapeHTML(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/**
 * Turn a pretty-printed JSON string into span-wrapped, syntax-highlighted HTML.
 * Token classes (tok-key/str/num/bool/null/punct) are styled in input.css. The
 * input MUST already be escaped-safe JSON (we generate it via JSON.stringify),
 * but we escape again defensively.
 * @param {string} json
 * @returns {string} HTML string
 */
export function highlightJSON(json) {
  const esc = escapeHTML(json);
  return esc.replace(
    /("(\\u[a-fA-F0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false)\b|\bnull\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
    (match) => {
      let cls = "tok-num";
      if (/^"/.test(match)) {
        cls = /:$/.test(match) ? "tok-key" : "tok-str";
      } else if (/true|false/.test(match)) {
        cls = "tok-bool";
      } else if (/null/.test(match)) {
        cls = "tok-null";
      }
      return `<span class="${cls}">${match}</span>`;
    },
  );
}

/**
 * Split a body into segments, marking base64/hex data runs long enough to be
 * expensive to render. A blob like an embedded base64 image or a large binary
 * field is one enormous token; putting it in a single text node makes the
 * browser line-break a multi-hundred-KB word, which is what freezes the
 * inspector. Callers render blob segments as a collapsed chip and expand one
 * only on an explicit click — the surrounding structure stays readable.
 *
 * Detection is by data alphabet (not whitespace-delimited words) so a minified
 * JSON body still collapses only its blob fields, leaving the keys and
 * punctuation visible.
 *
 * Joining `segments.map((s) => s.text)` always reproduces the input exactly.
 * `bytes` is the run's UTF-16 length — exact for the ASCII data this targets.
 *
 * @param {string} text
 * @param {{minRun?: number}} [opts] minimum run length to collapse (default 2 KiB)
 * @returns {Array<{text: string, blob: boolean, bytes: number, kind: string}>}
 */
export function collapseRuns(text, opts = {}) {
  const src = String(text ?? "");
  const minRun = Math.max(1, opts.minRun ?? 2048);
  if (!src || src.length < minRun) return [plainSegment(src)];
  const re = new RegExp(`[A-Za-z0-9+/]{${minRun},}={0,2}`, "g");
  const out = [];
  let last = 0;
  let m;
  while ((m = re.exec(src)) !== null) {
    const run = m[0];
    if (m.index > last) out.push(plainSegment(src.slice(last, m.index)));
    out.push({
      text: run,
      blob: true,
      bytes: run.length,
      // Look just behind the match so a data URL keeps its prefix label.
      kind: runKind(run, src.slice(Math.max(0, m.index - 9), m.index)),
    });
    last = m.index + run.length;
  }
  if (!out.length) return [plainSegment(src)];
  if (last < src.length) out.push(plainSegment(src.slice(last)));
  return out;
}

function plainSegment(text) {
  return { text, blob: false, bytes: text.length, kind: "" };
}

/**
 * Label a collapsible run so the chip can say what it is. All checks are
 * prefix-based or bounded regexes so this stays cheap on a multi-megabyte run.
 * @param {string} run
 * @param {string} before up to 9 chars immediately preceding the run
 * @returns {string}
 */
function runKind(run, before) {
  if (before.endsWith("base64,")) return "data URI";
  const image = imageKind(run);
  if (image) return image;
  if (run.length % 2 === 0 && /^[0-9a-fA-F]+$/.test(run)) return "hex";
  return "base64";
}

/**
 * Recognise common image magic bytes inside a base64 run without decoding it.
 * @param {string} run
 * @returns {string} media type, or "" when it is not a known image
 */
function imageKind(run) {
  if (run.startsWith("iVBORw0KGgo")) return "image/png";
  if (run.startsWith("/9j/")) return "image/jpeg";
  if (run.startsWith("R0lGOD")) return "image/gif";
  if (run.startsWith("UklGR")) return "image/webp";
  return "";
}
