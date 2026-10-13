import { useSyncExternalStore } from "react";

export type Pins = { ids: string[]; cols: number[]; rows: number[] };

export const maxPins = 9;

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
