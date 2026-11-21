import { useEffect, useLayoutEffect, useState, type HTMLAttributes, type RefObject } from "react";

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
    const clamped = clamp(Math.min(next, ceiling));
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

const composerKey = "chat-composer-height";
const composerEvent = "composer-height";

// A horizontal separator on the composer's top edge: drag up or ↑ grows the textarea, double-click resets.
// One height for every chat persists in localStorage; the default rows are the floor and 60% of the conversation the ceiling.
// ready is false until the textarea mounts (the conversation renders it only after loading).
export function useComposerResize(input: RefObject<HTMLTextAreaElement | null>, rows: number, ready: boolean, label: string) {
  const [saved, setSaved] = useState(() => Number(localStorage.getItem(composerKey)) || 0);
  const [floor, setFloor] = useState(0);
  const [room, setRoom] = useState(Infinity);

  useLayoutEffect(() => {
    const element = input.current;
    if (!element) return;
    const height = element.style.height;
    element.style.height = "";
    setFloor(element.offsetHeight);
    element.style.height = height;
  }, [input, rows, ready]);

  useEffect(() => {
    const pane = input.current?.closest(".conversation");
    if (!pane) return;
    const observer = new ResizeObserver(() => setRoom(Math.round(pane.clientHeight * 0.6)));
    observer.observe(pane);
    return () => observer.disconnect();
  }, [input, ready]);

  // Other mounted composers (grid panes, dock, pop-outs in this window) follow the shared height.
  useEffect(() => {
    const sync = (event: Event) => setSaved((event as CustomEvent<number>).detail);
    window.addEventListener(composerEvent, sync);
    return () => window.removeEventListener(composerEvent, sync);
  }, []);

  const ceiling = Math.max(floor, room);
  const height = Math.max(floor, Math.min(saved || floor, ceiling));

  function resize(next: number) {
    const clamped = Math.round(Math.min(Math.max(next, floor), ceiling));
    if (clamped <= floor) localStorage.removeItem(composerKey);
    else localStorage.setItem(composerKey, String(clamped));
    window.dispatchEvent(new CustomEvent(composerEvent, { detail: clamped <= floor ? 0 : clamped }));
  }

  const props: HTMLAttributes<HTMLDivElement> = {
    role: "separator",
    tabIndex: 0,
    "aria-label": label,
    "aria-orientation": "horizontal",
    "aria-valuemin": floor,
    "aria-valuemax": ceiling,
    "aria-valuenow": height,
    onPointerDown: (event) => {
      event.preventDefault();
      const element = event.currentTarget;
      element.focus();
      const start = height;
      const origin = event.clientY;
      element.setPointerCapture(event.pointerId);
      element.onpointermove = (moved) => resize(start + origin - moved.clientY);
      element.onlostpointercapture = () => (element.onpointermove = null);
    },
    onDoubleClick: () => resize(floor),
    onKeyDown: (event) => {
      const step = ({ ArrowUp: 24, ArrowDown: -24 } as Record<string, number>)[event.key];
      if (!step) return;
      event.preventDefault();
      resize(height + step);
    },
  };

  return { height: saved ? height : undefined, props };
}
