// Minimal line diff (LCS-based) used by the script editor to show what a
// transform changed in a payload. No external dependency; inputs are capped
// so a pathological body can't lock up the UI thread.

const MAX_LINES = 400;

/**
 * diffLines compares two texts line-by-line.
 * @returns {Array<{type: "same"|"add"|"del", text: string}>|null}
 *   null when either input is too large to diff comfortably.
 */
export function diffLines(before, after) {
  const a = String(before ?? "").split("\n");
  const b = String(after ?? "").split("\n");
  if (a.length > MAX_LINES || b.length > MAX_LINES) return null;

  // Standard LCS table; small inputs only (capped above).
  const m = a.length;
  const n = b.length;
  const lcs = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }

  const out = [];
  let i = 0;
  let j = 0;
  while (i < m && j < n) {
    if (a[i] === b[j]) {
      out.push({ type: "same", text: a[i] });
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      out.push({ type: "del", text: a[i] });
      i++;
    } else {
      out.push({ type: "add", text: b[j] });
      j++;
    }
  }
  for (; i < m; i++) out.push({ type: "del", text: a[i] });
  for (; j < n; j++) out.push({ type: "add", text: b[j] });
  return out;
}

/** hasChanges reports whether a diff contains any add/del line. */
export function hasChanges(diff) {
  return Array.isArray(diff) && diff.some((l) => l.type !== "same");
}
