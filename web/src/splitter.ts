import { useState, type HTMLAttributes } from "react";

// A vertical separator on a panel's left or right edge: drag, ←/→ in 24px steps, double-click resets; width persists in localStorage.
// limit caps the shown width to the room available right now without discarding the saved preference.
export function useSplitter({
  storageKey,
  defaultWidth,
  min,
  max,
  side,
  label,
  limit = Infinity,
}: {
  storageKey: string;
  defaultWidth: number;
  min: number;
  max: number;
  side: "left" | "right";
  label: string;
  limit?: number;
}) {
  const clamp = (value: number) => Math.min(max, Math.max(min, Math.round(value)));
  const [saved, setSaved] = useState(() => clamp(Number(localStorage.getItem(storageKey)) || defaultWidth));
  const ceiling = Math.max(0, Math.min(max, limit));
  const floor = Math.min(min, ceiling);
  const width = Math.min(saved, ceiling);
  const grow = side === "right" ? 1 : -1;

  function resize(next: number) {
    const clamped = Math.min(ceiling, clamp(next));
    localStorage.setItem(storageKey, String(clamped));
    setSaved(clamped);
  }

  const props: HTMLAttributes<HTMLDivElement> = {
    role: "separator",
    tabIndex: 0,
    "aria-label": label,
    "aria-orientation": "vertical",
    "aria-valuemin": floor,
    "aria-valuemax": ceiling,
    "aria-valuenow": width,
    onPointerDown: (event) => {
      event.preventDefault();
      const element = event.currentTarget;
      element.focus();
      const start = width;
      const origin = event.clientX;
      element.setPointerCapture(event.pointerId);
      element.onpointermove = (moved) => resize(start + grow * (moved.clientX - origin));
      element.onlostpointercapture = () => (element.onpointermove = null);
    },
    onDoubleClick: () => resize(defaultWidth),
    onKeyDown: (event) => {
      const step = ({ ArrowLeft: -24 * grow, ArrowRight: 24 * grow } as Record<string, number>)[event.key];
      if (!step) return;
      event.preventDefault();
      resize(width + step);
    },
  };

  return { width, props };
}
