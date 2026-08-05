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
    GET:     "bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/25",
    POST:    "bg-brand-500/15 text-brand-300 ring-1 ring-brand-500/25",
    PUT:     "bg-amber-500/15 text-amber-300 ring-1 ring-amber-500/25",
    PATCH:   "bg-amber-500/15 text-amber-300 ring-1 ring-amber-500/25",
    DELETE:  "bg-rose-500/15 text-rose-300 ring-1 ring-rose-500/25",
    HEAD:    "bg-neutral-500/15 text-neutral-300 ring-1 ring-neutral-500/25",
    OPTIONS: "bg-neutral-500/15 text-neutral-300 ring-1 ring-neutral-500/25",
  };
  return map[(method || "").toUpperCase()] || "bg-neutral-700/40 text-neutral-300 ring-1 ring-neutral-600/40";
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
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
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
