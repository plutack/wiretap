// Display preferences (font scale, row density). Purely presentational and
// device-local, so they live in localStorage rather than config.yaml — the
// same laptop config can drive a HiDPI external monitor differently.
//
// applyDisplayPrefs drives everything through CSS: --ui-font-scale multiplies
// the root font size (layout.css keys every size off rem), and the
// density-compact body class tightens table rows.

const KEY = "wiretap.display";

export const FONT_SCALES = [
  { value: "1", label: "Default" },
  { value: "1.1", label: "Large" },
  { value: "1.22", label: "Extra large" },
];

export const DENSITIES = [
  { value: "comfortable", label: "Comfortable" },
  { value: "compact", label: "Compact" },
];

export function loadDisplayPrefs() {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || "{}");
    return {
      fontScale: String(raw.fontScale || "1"),
      density: raw.density === "compact" ? "compact" : "comfortable",
    };
  } catch {
    return { fontScale: "1", density: "comfortable" };
  }
}

export function applyDisplayPrefs(prefs) {
  document.documentElement.style.setProperty("--ui-font-scale", prefs.fontScale || "1");
  document.body.classList.toggle("density-compact", prefs.density === "compact");
}

export function saveDisplayPrefs(prefs) {
  try {
    localStorage.setItem(KEY, JSON.stringify(prefs));
  } catch {
    // Private-mode storage failures degrade to session-only prefs.
  }
  applyDisplayPrefs(prefs);
}
