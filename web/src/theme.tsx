import { useSyncExternalStore } from "react";
import { api } from "./api";
import { IconButton } from "./icons";
import { useText } from "./i18n";

const text = {
  en: {
    switchLight: "Switch to light theme",
    switchDark: "Switch to dark theme",
  },
  es: {
    switchLight: "Cambiar al tema claro",
    switchDark: "Cambiar al tema oscuro",
  },
};

export type Mode = "system" | "light" | "dark";
export type SidebarStyle = "flat" | "gradient";
export const palettes = [
  { id: "patio", name: "Patio" },
  { id: "dusk", name: "Dusk" },
  { id: "orchid", name: "Orchid" },
  { id: "berry", name: "Berry" },
  { id: "flamingo", name: "Flamingo" },
  { id: "mango", name: "Mango" },
  { id: "lagoon", name: "Lagoon" },
  { id: "grape", name: "Grape Soda" },
  { id: "lime", name: "Lime Pop" },
  { id: "custom", name: "Custom" },
] as const;
export type Palette = (typeof palettes)[number]["id"];
export type CustomTheme = { primary: string; secondary: string; base: string; sidebar_from: string; sidebar_to: string };
type Prefs = { mode: Mode; palette: Palette; style: SidebarStyle; custom: CustomTheme };

// Patio's own colours, so a fresh custom theme starts from the default look.
export const defaultCustomTheme: CustomTheme = {
  primary: "#f84b9f",
  secondary: "#f84b9f",
  base: "#fbf0e4",
  sidebar_from: "#b10b69",
  sidebar_to: "#c2460d",
};

const dark = matchMedia("(prefers-color-scheme: dark)");
const listeners = new Set<() => void>();

function read(): Prefs {
  const mode = localStorage.getItem("theme");
  const palette = localStorage.getItem("palette");
  let custom = defaultCustomTheme;
  try {
    custom = { ...custom, ...JSON.parse(localStorage.getItem("custom-theme") ?? "{}") };
  } catch {
    // A broken cache falls back to the defaults; the DB copy loads in initTheme.
  }
  return {
    mode: mode === "light" || mode === "dark" ? mode : "system",
    palette: palettes.find((p) => p.id === palette)?.id ?? "patio",
    style: localStorage.getItem("sidebar-style") === "flat" ? "flat" : "gradient",
    custom,
  };
}

// sRGB hex → oklch [lightness 0–1, chroma, hue°] (Björn Ottosson's OKLab matrices).
function oklch(hex: string): [number, number, number] {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  const A = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const B = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  return [0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s, Math.hypot(A, B), ((Math.atan2(B, A) * 180) / Math.PI + 360) % 360];
}

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));
const color = (l: number, c: number, h: number) => `oklch(${(l * 100).toFixed(1)}% ${c.toFixed(3)} ${h.toFixed(1)})`;

// WCAG relative luminance of an oklch colour, clipped to sRGB.
function luminance(L: number, C: number, H: number) {
  const a = C * Math.cos((H * Math.PI) / 180);
  const b = C * Math.sin((H * Math.PI) / 180);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const [r, g, bl] = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ].map((v) => clamp(v, 0, 1));
  return 0.2126 * r + 0.7152 * g + 0.0722 * bl;
}

// Label colour on the primary fill: the tinted dark or light text with more contrast,
// or plain black/white when neither clears WCAG AA (4.5:1) with margin for CSS rounding.
function primaryContent(l: number, c: number, h: number) {
  const fill = luminance(l, c, h);
  const contrast = (y: number) => (Math.max(fill, y) + 0.05) / (Math.min(fill, y) + 0.05);
  const darkText = contrast(luminance(0.18, 0.05, h));
  const lightText = contrast(luminance(0.97, 0.02, h));
  if (Math.max(darkText, lightText) >= 4.6) return darkText >= lightText ? color(0.18, 0.05, h) : color(0.97, 0.02, h);
  return contrast(0) >= contrast(1) ? color(0, 0, 0) : color(1, 0, 0);
}

// Same recipe as the built-in palettes in styles.css: the picked hue and chroma, with
// lightness fixed per mode so text and the white sidebar labels stay readable.
function customVars({ primary, secondary, base, sidebar_from, sidebar_to }: CustomTheme, mode: "light" | "dark") {
  const [pl, pc, ph] = oklch(primary);
  const [, sc, sh] = oklch(secondary);
  const [, bc, bh] = oklch(base);
  const tint = clamp(bc / 0.02, 0, 1);
  const sidebar = (hex: string) => {
    const [l, c, h] = oklch(hex);
    return mode === "light" ? color(clamp(l, 0.3, 0.56), Math.min(c, 0.2), h) : color(clamp(l * 0.72, 0.24, 0.4), c * 0.65, h);
  };
  const fill = mode === "light" ? clamp(pl, 0.45, 0.85) : clamp(pl + 0.08, 0.72, 0.88);
  const fillChroma = Math.min(pc, mode === "light" ? 0.22 : 0.17);
  const vars = {
    "--primary": color(fill, fillChroma, ph),
    "--primary-content": primaryContent(fill, fillChroma, ph),
    "--primary-text": mode === "light" ? color(0.5, Math.min(pc, 0.2), ph) : color(0.8, Math.min(pc, 0.13), ph),
    "--secondary": mode === "light" ? color(0.62, Math.min(sc, 0.14), sh) : color(0.76, Math.min(sc, 0.12), sh),
    "--secondary-text": mode === "light" ? color(0.48, Math.min(sc, 0.14), sh) : color(0.8, Math.min(sc, 0.11), sh),
    "--sidebar-from": sidebar(sidebar_from),
    "--sidebar-to": sidebar(sidebar_to),
    ...(mode === "light"
      ? {
          "--base-100": color(0.986, 0.012 * tint, bh),
          "--base-200": color(0.96, 0.02 * tint, bh),
          "--base-300": color(0.925, 0.028 * tint, bh),
          "--neutral": color(0.89, 0.03 * tint, bh),
          "--content": color(0.22, 0.03 * tint, bh),
          "--neutral-content": color(0.24, 0.03 * tint, bh),
        }
      : {
          "--base-100": color(0.24, 0.035 * tint, bh),
          "--base-200": color(0.215, 0.032 * tint, bh),
          "--base-300": color(0.19, 0.03 * tint, bh),
          "--neutral": color(0.36, 0.04 * tint, bh),
          "--content": color(0.96, 0.01 * tint, bh),
          "--neutral-content": color(0.9, 0.01 * tint, bh),
        }),
  };
  return Object.entries(vars)
    .map(([name, value]) => `${name}: ${value};`)
    .join(" ");
}

// A generated stylesheet, not inline vars, so light/dark switching and the Settings swatch work like the built-ins.
function applyCustom() {
  let style = document.getElementById("custom-theme");
  if (!style) {
    style = document.createElement("style");
    style.id = "custom-theme";
    document.head.append(style);
  }
  style.textContent =
    `[data-palette="custom"] { ${customVars(prefs.custom, "light")} }\n` +
    `[data-theme="dark"][data-palette="custom"], [data-theme="dark"] [data-palette="custom"] { ${customVars(prefs.custom, "dark")} }`;
}

/** Previews and caches a custom theme; the caller saves it to the DB. */
export function setCustomTheme(custom: CustomTheme) {
  prefs = { ...prefs, custom };
  localStorage.setItem("custom-theme", JSON.stringify(custom));
  applyCustom();
  listeners.forEach((listener) => listener());
}

let prefs = read();

function resolved(): "light" | "dark" {
  if (prefs.mode !== "system") return prefs.mode;
  return dark.matches ? "dark" : "light";
}

function apply() {
  const root = document.documentElement.dataset;
  root.theme = resolved();
  root.palette = prefs.palette;
  root.style = prefs.style;
  listeners.forEach((listener) => listener());
}

export function setThemePrefs(change: Partial<Prefs>) {
  prefs = { ...prefs, ...change };
  localStorage.setItem("theme", prefs.mode);
  localStorage.setItem("palette", prefs.palette);
  localStorage.setItem("sidebar-style", prefs.style);
  apply();
}

export function initTheme() {
  applyCustom();
  apply();
  const cached = prefs.custom;
  api
    .get<CustomTheme | null>("/api/settings/custom-theme")
    // An edit made while this loads is newer than the DB copy; keep it.
    .then((saved) => saved && prefs.custom === cached && setCustomTheme(saved))
    .catch((reason) => console.error("load custom theme", reason));
  dark.addEventListener("change", () => {
    if (prefs.mode !== "system") return;
    prefs = { ...prefs };
    apply();
  });
}

export function useThemePrefs() {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => prefs,
  );
}

export function ThemeToggle() {
  useThemePrefs();
  const t = useText(text);
  const theme = resolved();
  return (
    <IconButton
      className="ghost"
      icon={theme === "dark" ? "sun" : "moon"}
      label={theme === "dark" ? t.switchLight : t.switchDark}
      onClick={() => setThemePrefs({ mode: theme === "dark" ? "light" : "dark" })}
    />
  );
}
