// Shared formatting helpers used across all components.

/**
 * Return the Tailwind classes for a HTTP method badge.
 * @param {string} method
 * @returns {string}
 */
export function methodBadgeClass(method) {
  const map = {
    GET:    "bg-emerald-900/60 text-emerald-300",
    POST:   "bg-sky-900/60 text-sky-300",
    PUT:    "bg-amber-900/60 text-amber-300",
    PATCH:  "bg-amber-900/60 text-amber-300",
    DELETE: "bg-rose-900/60 text-rose-300",
  };
  return map[(method || "").toUpperCase()] || "bg-neutral-800 text-neutral-300";
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
  if (n < 1024) return n + "b";
  return (n / 1024).toFixed(1) + "kb";
}
