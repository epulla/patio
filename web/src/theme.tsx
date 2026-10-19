import { useSyncExternalStore } from "react";
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
  { id: "mango", name: "Mango" },
  { id: "lagoon", name: "Lagoon" },
  { id: "grape", name: "Grape Soda" },
  { id: "lime", name: "Lime Pop" },
] as const;
export type Palette = (typeof palettes)[number]["id"];
type Prefs = { mode: Mode; palette: Palette; style: SidebarStyle };

const dark = matchMedia("(prefers-color-scheme: dark)");
const listeners = new Set<() => void>();

function read(): Prefs {
  const mode = localStorage.getItem("theme");
  const palette = localStorage.getItem("palette");
  return {
    mode: mode === "light" || mode === "dark" ? mode : "system",
    palette: palettes.find((p) => p.id === palette)?.id ?? "patio",
    style: localStorage.getItem("sidebar-style") === "flat" ? "flat" : "gradient",
  };
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
  apply();
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
