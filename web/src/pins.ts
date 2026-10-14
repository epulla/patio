import { useSyncExternalStore } from "react";

export type Pins = { ids: string[]; cols: number[]; rows: number[] };

export const maxPins = 9;

const mac = /Mac|iPhone|iPad/.test(navigator.platform);
export const pinShortcut = (n: number) => (mac ? `⌘${n}` : `Ctrl+${n}`);
export const pinAriaShortcut = (n: number) => (mac ? `Meta+${n}` : `Control+${n}`);

export const unpinShortcut = mac ? "⌘⇧U" : "Ctrl+Shift+U";
export const unpinAriaShortcut = mac ? "Meta+Shift+U" : "Control+Shift+U";
export const isUnpinKey = (event: KeyboardEvent) =>
  (event.metaKey || event.ctrlKey) && event.shiftKey && !event.altKey && event.key.toLowerCase() === "u";

export function unpin(id: string) {
  setPins((pins) => pinLayout(pins.ids.filter((pinned) => pinned !== id)));
}

// Returns N for Cmd/Ctrl+N when N is a pinned position, else 0.
export function pinNumber(event: KeyboardEvent, count: number) {
  if (!(event.metaKey || event.ctrlKey) || event.altKey || event.shiftKey) return 0;
  const n = Number(event.key);
  return Number.isInteger(n) && n >= 1 && n <= count ? n : 0;
}

export function pinLayout(ids: string[]): Pins {
  const cols = Math.ceil(Math.sqrt(ids.length));
  const rows = cols && Math.ceil(ids.length / cols);
  return { ids, cols: Array(cols).fill(1 / cols), rows: Array(rows).fill(1 / rows) };
}

function loadPins(): Pins {
  try {
    const saved = JSON.parse(localStorage.getItem("chat-pins") ?? "null") as Partial<Pins> | null;
    const ids: unknown = saved?.ids;
    const cols: unknown = saved?.cols;
    const rows: unknown = saved?.rows;
    if (!Array.isArray(ids) || !ids.every((x) => typeof x === "string")) return pinLayout([]);
    const pins = pinLayout(ids.slice(0, maxPins));
    const fits = (sizes: unknown, fresh: number[]): sizes is number[] =>
      Array.isArray(sizes) && sizes.length === fresh.length && sizes.every((x) => typeof x === "number" && x > 0);
    return fits(cols, pins.cols) && fits(rows, pins.rows) ? { ...pins, cols, rows } : pins;
  } catch {
    return pinLayout([]);
  }
}

let current = loadPins();
const listeners = new Set<() => void>();

export function setPins(next: Pins | ((pins: Pins) => Pins)) {
  current = typeof next === "function" ? next(current) : next;
  localStorage.setItem("chat-pins", JSON.stringify(current));
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function usePins() {
  return useSyncExternalStore(subscribe, () => current);
}
