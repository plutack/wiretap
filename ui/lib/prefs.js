// Display preferences (theme, font scale, row density). Purely presentational and
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

export const THEMES = [
  {
    value: "system",
    label: "System",
    description: "Wiretap light or dark, following your desktop.",
    colors: ["#f2f5f6", "#17242a", "#087f6d"],
  },
  {
    value: "wiretap-dark",
    label: "Wiretap Dark",
    description: "The original charcoal workbench with a mint signal colour.",
    colors: ["#080a0c", "#12161a", "#62d9bf"],
  },
  {
    value: "wiretap-light",
    label: "Wiretap Light",
    description: "A low-glare light workbench with strong content contrast.",
    colors: ["#f2f5f6", "#ffffff", "#087f6d"],
  },
  {
    value: "nord",
    label: "Nord",
    description: "Arctic blue surfaces using the Nord palette.",
    colors: ["#2e3440", "#3b4252", "#88c0d0"],
  },
  {
    value: "catppuccin-mocha",
    label: "Catppuccin Mocha",
    description: "A soft dark pastel palette with teal signals.",
    colors: ["#11111b", "#313244", "#94e2d5"],
  },
  {
    value: "catppuccin-latte",
    label: "Catppuccin Latte",
    description: "Catppuccin's calm light palette with a teal accent.",
    colors: ["#eff1f5", "#ffffff", "#179299"],
  },
];

const THEME_VALUES = new Set(THEMES.map((theme) => theme.value));
const systemAppearance = window.matchMedia("(prefers-color-scheme: dark)");
let activePrefs = null;

function normalizedTheme(value) {
  return THEME_VALUES.has(value) ? value : "system";
}

function appearanceFor(theme) {
  if (theme === "system") return systemAppearance.matches ? "dark" : "light";
  return theme === "wiretap-light" || theme === "catppuccin-latte" ? "light" : "dark";
}

export function loadDisplayPrefs() {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || "{}");
    return {
      fontScale: String(raw.fontScale || "1"),
      density: raw.density === "compact" ? "compact" : "comfortable",
      theme: normalizedTheme(raw.theme),
    };
  } catch {
    return { fontScale: "1", density: "comfortable", theme: "system" };
  }
}

export function applyDisplayPrefs(prefs) {
  activePrefs = { ...prefs, theme: normalizedTheme(prefs.theme) };
  const appearance = appearanceFor(activePrefs.theme);
  document.documentElement.dataset.theme = activePrefs.theme;
  document.documentElement.dataset.appearance = appearance;
  document.documentElement.style.colorScheme = appearance;
  document.documentElement.style.setProperty("--ui-font-scale", prefs.fontScale || "1");
  document.body.classList.toggle("density-compact", prefs.density === "compact");
  document.dispatchEvent(
    new CustomEvent("wiretap-theme-change", {
      detail: { theme: activePrefs.theme, appearance },
    }),
  );
}

export function saveDisplayPrefs(prefs) {
  try {
    localStorage.setItem(KEY, JSON.stringify(prefs));
  } catch {
    // Private-mode storage failures degrade to session-only prefs.
  }
  applyDisplayPrefs(prefs);
}

const reapplySystemTheme = () => {
  if (activePrefs?.theme === "system") applyDisplayPrefs(activePrefs);
};
if (systemAppearance.addEventListener)
  systemAppearance.addEventListener("change", reapplySystemTheme);
else systemAppearance.addListener(reapplySystemTheme);
