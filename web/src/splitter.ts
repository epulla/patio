import { useState, type HTMLAttributes } from "react";

// A vertical separator on a panel's left or right edge: drag, ←/→ in 24px steps, double-click resets; width persists in localStorage.
export function useSplitter({
  storageKey,
  defaultWidth,
  min,
  max,
  side,
  label,
}: {
  storageKey: string;
  defaultWidth: number;
  min: number;
  max: number;
  side: "left" | "right";
  label: string;
}) {
  const clamp = (value: number) => Math.min(max, Math.max(min, Math.round(value)));
  const [width, setWidth] = useState(() => clamp(Number(localStorage.getItem(storageKey)) || defaultWidth));
  const grow = side === "right" ? 1 : -1;

  function resize(next: number) {
    const clamped = clamp(next);
    localStorage.setItem(storageKey, String(clamped));
    setWidth(clamped);
  }

  const props: HTMLAttributes<HTMLDivElement> = {
    role: "separator",
    tabIndex: 0,
    "aria-label": label,
    "aria-orientation": "vertical",
    "aria-valuemin": min,
    "aria-valuemax": max,
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
