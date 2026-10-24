import { useSyncExternalStore, type CSSProperties } from "react";

export type Pins = { ids: string[]; cols: number[]; rows: number[] };
// Presets rather than a free picker, so every color reads in both themes.
export const pinColors = {
  pink: "var(--primary)",
  red: "oklch(63% 0.2 25)",
  orange: "oklch(70% 0.17 55)",
  yellow: "oklch(76% 0.15 90)",
  green: "oklch(66% 0.16 150)",
  teal: "oklch(66% 0.11 195)",
  blue: "oklch(62% 0.16 255)",
  purple: "oklch(62% 0.18 300)",
};
export type PinColor = keyof typeof pinColors;
const colorKeys = Object.keys(pinColors) as PinColor[];
// Sets --pin, which pin icons, badges and dots read.
export const pinStyle = (color: PinColor) => ({ "--pin": pinColors[color] }) as CSSProperties;

export type PinGroup = Pins & { id: string; name: string; color: PinColor };
export type PinGroups = { groups: PinGroup[]; active: string };

export const maxPins = 9;

const mac = /Mac|iPhone|iPad/.test(navigator.platform);
export const pinShortcut = (n: number) => (mac ? `⌘${n}` : `Ctrl+${n}`);
export const pinAriaShortcut = (n: number) => (mac ? `Meta+${n}` : `Control+${n}`);

export const unpinShortcut = mac ? "⌘⇧U" : "Ctrl+Shift+U";
export const unpinAriaShortcut = mac ? "Meta+Shift+U" : "Control+Shift+U";
export const isUnpinKey = (event: KeyboardEvent) =>
  (event.metaKey || event.ctrlKey) && event.shiftKey && !event.altKey && event.key.toLowerCase() === "u";

export const newPinnedShortcut = mac ? "⌘⇧N" : "Ctrl+Shift+N";
export const newPinnedAriaShortcut = mac ? "Meta+Shift+N" : "Control+Shift+N";
export const isNewPinnedKey = (event: KeyboardEvent) =>
  (event.metaKey || event.ctrlKey) && event.shiftKey && !event.altKey && event.key.toLowerCase() === "n";
// Asks App to start a chat pinned into the active group.
export const requestNewPinnedChat = () => dispatchEvent(new Event("new-pinned-chat"));

export const groupShortcuts = mac ? "⌘⇧[ / ⌘⇧]" : "Ctrl+Shift+[ / Ctrl+Shift+]";
export const groupAriaShortcuts = mac ? "Meta+Shift+[ Meta+Shift+]" : "Control+Shift+[ Control+Shift+]";
// Returns -1 or 1 for Cmd/Ctrl+Shift+[ or ], else 0. event.code, since Shift turns the key into { or }.
export function groupStep(event: KeyboardEvent) {
  if (!(event.metaKey || event.ctrlKey) || !event.shiftKey || event.altKey) return 0;
  return event.code === "BracketLeft" ? -1 : event.code === "BracketRight" ? 1 : 0;
}

// Removes the chat from whichever group holds it.
export function unpin(id: string) {
  update((store) => ({
    ...store,
    groups: store.groups.map((group) => (group.ids.includes(id) ? { ...group, ...pinLayout(group.ids.filter((x) => x !== id)) } : group)),
  }));
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

const newGroup = (name: string, ids: string[] = [], color: PinColor = "pink"): PinGroup => ({
  id: Date.now().toString(36) + Math.random().toString(36).slice(2),
  name,
  color,
  ...pinLayout(ids),
});

function loadGroup(saved: Partial<PinGroup> | null): PinGroup | undefined {
  const ids: unknown = saved?.ids;
  const cols: unknown = saved?.cols;
  const rows: unknown = saved?.rows;
  if (!Array.isArray(ids) || !ids.every((x) => typeof x === "string")) return;
  const group = { ...newGroup(typeof saved?.name === "string" ? saved.name : "Pinned", ids.slice(0, maxPins)) };
  if (typeof saved?.id === "string") group.id = saved.id;
  if (colorKeys.includes(saved?.color as PinColor)) group.color = saved!.color!;
  const fits = (sizes: unknown, fresh: number[]): sizes is number[] =>
    Array.isArray(sizes) && sizes.length === fresh.length && sizes.every((x) => typeof x === "number" && x > 0);
  return fits(cols, group.cols) && fits(rows, group.rows) ? { ...group, cols, rows } : group;
}

function load(): PinGroups {
  try {
    const saved = JSON.parse(localStorage.getItem("chat-pin-groups") ?? "null") as Partial<PinGroups> | null;
    const groups = Array.isArray(saved?.groups) ? saved.groups.map(loadGroup).filter((group) => !!group) : [];
    if (groups.length) return { groups, active: groups.some((group) => group.id === saved?.active) ? saved!.active! : groups[0].id };
    // Before groups, pins were one list under chat-pins; it becomes the first group.
    const legacy = loadGroup(JSON.parse(localStorage.getItem("chat-pins") ?? "null")) ?? newGroup("Pinned");
    return { groups: [legacy], active: legacy.id };
  } catch {
    const group = newGroup("Pinned");
    return { groups: [group], active: group.id };
  }
}

let store = load();
const listeners = new Set<() => void>();

function update(next: (store: PinGroups) => PinGroups) {
  store = next(store);
  localStorage.setItem("chat-pin-groups", JSON.stringify(store));
  listeners.forEach((listener) => listener());
}

const activeGroup = () => store.groups.find((group) => group.id === store.active)!;

// Pins of the active group.
export function setPins(next: Pins | ((pins: Pins) => Pins)) {
  update((store) => ({
    ...store,
    groups: store.groups.map((group) => {
      if (group.id !== store.active) return group;
      const pins = typeof next === "function" ? next(group) : next;
      return { ...group, ids: pins.ids, cols: pins.cols, rows: pins.rows };
    }),
  }));
}

export const getPins = (): Pins => activeGroup();
export const allPinnedIds = () => store.groups.flatMap((group) => group.ids);
export const groupOf = (chatID: string) => store.groups.find((group) => group.ids.includes(chatID));

export function setActiveGroup(id: string) {
  if (id !== store.active && store.groups.some((group) => group.id === id)) update((store) => ({ ...store, active: id }));
}

export function cycleGroup(step: number) {
  const index = store.groups.findIndex((group) => group.id === store.active);
  setActiveGroup(store.groups[(index + step + store.groups.length) % store.groups.length].id);
}

export function addGroup(name: string) {
  const used = new Set(store.groups.map((group) => group.color));
  const group = newGroup(name, [], colorKeys.find((color) => !used.has(color)) ?? colorKeys[store.groups.length % colorKeys.length]);
  update((store) => ({ groups: [...store.groups, group], active: group.id }));
  return group.id;
}

export function renameGroup(id: string, name: string) {
  update((store) => ({ ...store, groups: store.groups.map((group) => (group.id === id ? { ...group, name } : group)) }));
}

export function setGroupColor(id: string, color: PinColor) {
  update((store) => ({ ...store, groups: store.groups.map((group) => (group.id === id ? { ...group, color } : group)) }));
}

// Deleting a group only unpins its chats; the last group can't be deleted.
export function deleteGroup(id: string) {
  if (store.groups.length < 2) return;
  update((store) => {
    const groups = store.groups.filter((group) => group.id !== id);
    return { groups, active: store.active === id ? groups[0].id : store.active };
  });
}

// Pins the chat into the target group, taking it out of any other. False when the target is full.
export function moveToGroup(chatID: string, id: string) {
  const target = store.groups.find((group) => group.id === id);
  if (!target || target.ids.includes(chatID)) return true;
  if (target.ids.length >= maxPins) return false;
  update((store) => ({
    ...store,
    groups: store.groups.map((group) =>
      group.id === id
        ? { ...group, ...pinLayout([...group.ids, chatID]) }
        : group.ids.includes(chatID)
          ? { ...group, ...pinLayout(group.ids.filter((x) => x !== chatID)) }
          : group,
    ),
  }));
  return true;
}

export const pin = (chatID: string) => moveToGroup(chatID, store.active);

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// Last focused pinned chat, so the side panel reopens on it instead of the first pin.
export const lastPinnedChat = () => localStorage.getItem("chat-dock-active") ?? undefined;
export const rememberPinnedChat = (id: string) => localStorage.setItem("chat-dock-active", id);

export const usePinGroups = () => useSyncExternalStore(subscribe, () => store);

export function usePins(): Pins {
  return useSyncExternalStore(subscribe, activeGroup);
}
