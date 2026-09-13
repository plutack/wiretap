// Microlighter adapter. The desktop UI can run on older WebKitGTK builds, so
// syntax highlighting remains an enhancement and never blocks body rendering.

let loadPromise = null;
let scheduled = false;
let warningShown = false;

export function supportsSyntaxHighlights() {
  return typeof CSS !== "undefined"
    && "highlights" in CSS
    && typeof Highlight !== "undefined"
    && typeof Range !== "undefined";
}

export function requestSyntaxHighlight() {
  if (!supportsSyntaxHighlights() || scheduled) return;
  scheduled = true;
  queueMicrotask(async () => {
    scheduled = false;
    try {
      loadPromise ||= import("../vendor/microlighter/highlight.js");
      const { highlightAll } = await loadPromise;
      await highlightAll({
        root: document,
        selector: "pre.body-code > code[data-language]",
      });
    } catch (error) {
      // The unhighlighted text remains fully usable. Log once for diagnostics
      // without surfacing a toast for an optional visual enhancement.
      if (!warningShown) {
        console.warn("wiretap syntax highlighting unavailable", error);
        warningShown = true;
      }
      loadPromise = null;
    }
  });
}
