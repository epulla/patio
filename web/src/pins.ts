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

// How the Chats page shows a group: the selected tab only, every tab in a grid, or a custom split.
export type PinView = "one" | "grid" | "split";
const views: PinView[] = ["one", "grid", "split"];
// A split is a binary tree: a leaf shows one chat; a node splits side by side (row) or stacked (column), ratio being a's share.
export type SplitNode = { chat: string } | { dir: "row" | "column"; ratio: number; a: SplitNode; b: SplitNode };
export type SplitEdge = "left" | "right" | "top" | "bottom";
export const maxPanes = 4;
export type PinGroup = Pins & { id: string; name: string; color: PinColor; view: PinView; split?: SplitNode };
export type PinGroups = { groups: PinGroup[]; active: string };

export const maxPins = 9;

const mac = /Mac|iPhone|iPad/.test(navigator.platform);
export const pinShortcut = (n: number) => (mac ? `⌘${n}` : `Ctrl+${n}`);
export const pinAriaShortcut = (n: number) => (mac ? `Meta+${n}` : `Control+${n}`);

// Cmd/Ctrl+W closes (unpins) a tab like a browser; Cmd/Ctrl+Shift+U still works. Patio's macOS menu has no Close Window item, so Cmd+W reaches the page.
export const unpinShortcut = mac ? "⌘W" : "Ctrl+W";
export const unpinAriaShortcut = mac ? "Meta+W" : "Control+W";
export function isUnpinKey(event: KeyboardEvent) {
  if (!(event.metaKey || event.ctrlKey) || event.altKey) return false;
  const key = event.key.toLowerCase();
  return event.shiftKey ? key === "u" : key === "w";
}

export const newPinnedShortcut = mac ? "⌘⇧N" : "Ctrl+Shift+N";
export const newPinnedAriaShortcut = mac ? "Meta+Shift+N" : "Control+Shift+N";
export const isNewPinnedKey = (event: KeyboardEvent) =>
  (event.metaKey || event.ctrlKey) && event.shiftKey && !event.altKey && event.key.toLowerCase() === "n";
// Asks App to start a chat pinned into the active group.
export const requestNewPinnedChat = () => dispatchEvent(new Event("new-pinned-chat"));

export const groupShortcuts = mac ? "⌘⌥↑ / ⌘⌥↓" : "Ctrl+Alt+↑ / Ctrl+Alt+↓";
export const groupAriaShortcuts = mac ? "Meta+Alt+ArrowUp Meta+Alt+ArrowDown" : "Control+Alt+ArrowUp Control+Alt+ArrowDown";
// Returns -1 or 1 for Cmd/Ctrl+Alt+Up or Down, else 0.
export function groupStep(event: KeyboardEvent) {
  if (!(event.metaKey || event.ctrlKey) || !event.altKey || event.shiftKey) return 0;
  return event.key === "ArrowUp" ? -1 : event.key === "ArrowDown" ? 1 : 0;
}

// Returns -1 or 1 for Cmd/Ctrl+Shift+[ or ], else 0. event.code, since Shift turns the key into { or }.
export function tabStep(event: KeyboardEvent) {
  if (!(event.metaKey || event.ctrlKey) || !event.shiftKey || event.altKey) return 0;
  return event.code === "BracketLeft" ? -1 : event.code === "BracketRight" ? 1 : 0;
}

// The tab step positions away from index, wrapping; from an unpinned chat (-1) it starts at either end.
export const stepIndex = (index: number, step: number, count: number) =>
  index < 0 ? (step > 0 ? 0 : count - 1) : (index + step + count) % count;

// Removes the chat from whichever group holds it.
export function unpin(id: string) {
  update((store) => ({
    ...store,
    groups: store.groups.map((group) => (group.ids.includes(id) ? withIds(group, group.ids.filter((x) => x !== id)) : group)),
  }));
}

// New pins reset the grid sizes; panes of unpinned chats collapse.
const withIds = (group: PinGroup, ids: string[]): PinGroup => ({ ...group, ...pinLayout(ids), split: pruneSplit(group.split, ids) });

export const splitLeaves = (node: SplitNode | undefined): string[] =>
  !node ? [] : "chat" in node ? [node.chat] : [...splitLeaves(node.a), ...splitLeaves(node.b)];

// Validates a saved split and drops leaves not in ids, so a pane's sibling takes its place. Undefined when nothing is left.
export function pruneSplit(node: unknown, ids: string[], seen = new Set<string>()): SplitNode | undefined {
  if (!node || typeof node !== "object") return;
  const value = node as Record<string, unknown>;
  if (typeof value.chat === "string") {
    if (!ids.includes(value.chat) || seen.has(value.chat)) return;
    seen.add(value.chat);
    return { chat: value.chat };
  }
  if ((value.dir !== "row" && value.dir !== "column") || typeof value.ratio !== "number" || !(value.ratio > 0 && value.ratio < 1)) return;
  const a = pruneSplit(value.a, ids, seen);
  const b = pruneSplit(value.b, ids, seen);
  const pruned = a && b ? { dir: value.dir as "row" | "column", ratio: value.ratio, a, b } : a ?? b;
  return splitLeaves(pruned).length > maxPanes ? undefined : pruned;
}

const mapLeaves = (node: SplitNode, leaf: (chat: string) => SplitNode): SplitNode =>
  "chat" in node ? leaf(node.chat) : { ...node, a: mapLeaves(node.a, leaf), b: mapLeaves(node.b, leaf) };

// Shows chat in target's pane; if chat already has a pane, the two swap.
export const replaceLeaf = (node: SplitNode, target: string, chat: string) =>
  mapLeaves(node, (leaf) => ({ chat: leaf === target ? chat : leaf === chat ? target : leaf }));

// Moves chat (placed or not) to an edge of target's pane.
export function splitLeaf(node: SplitNode, target: string, edge: SplitEdge, chat: string) {
  const rest = pruneSplit(node, splitLeaves(node).filter((leaf) => leaf !== chat))!;
  const before = edge === "left" || edge === "top";
  return mapLeaves(rest, (leaf) =>
    leaf !== target
      ? { chat: leaf }
      : { dir: edge === "left" || edge === "right" ? "row" : "column", ratio: 0.5, a: { chat: before ? chat : leaf }, b: { chat: before ? leaf : chat } },
  );
}

// path is the a/b steps from the root to the node.
export function setSplitRatio(node: SplitNode, path: string, ratio: number): SplitNode {
  if ("chat" in node) return node;
  if (!path) return { ...node, ratio };
  return path[0] === "a" ? { ...node, a: setSplitRatio(node.a, path.slice(1), ratio) } : { ...node, b: setSplitRatio(node.b, path.slice(1), ratio) };
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
  view: "grid",
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
  if (views.includes(saved?.view as PinView)) group.view = saved!.view!;
  group.split = pruneSplit(saved?.split, group.ids);
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

// Moves a tab of the active group to index (counted before the move, like a browser insertion point).
export function moveTab(chatID: string, index: number) {
  setPins((pins) => {
    const from = pins.ids.indexOf(chatID);
    if (from < 0) return pins;
    const ids = pins.ids.filter((id) => id !== chatID);
    ids.splice(Math.max(0, Math.min(ids.length, index > from ? index - 1 : index)), 0, chatID);
    return { ...pins, ids };
  });
}

// Returns -1 or 1 for Cmd/Ctrl+Alt+Left or Right, which move the current tab.
export function tabMoveStep(event: KeyboardEvent) {
  if (!(event.metaKey || event.ctrlKey) || !event.altKey || event.shiftKey) return 0;
  return event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0;
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

export function setGroupView(id: string, view: PinView) {
  update((store) => ({ ...store, groups: store.groups.map((group) => (group.id === id ? { ...group, view } : group)) }));
}

export function setGroupSplit(id: string, split: SplitNode) {
  update((store) => ({ ...store, groups: store.groups.map((group) => (group.id === id ? { ...group, split } : group)) }));
}

export function setGroupColor(id: string, color: PinColor) {
  update((store) => ({ ...store, groups: store.groups.map((group) => (group.id === id ? { ...group, color } : group)) }));
}

export const groupDragType = "application/x-pin-group";

export function setGroupOrder(ids: string[]) {
  update((store) => ({ ...store, groups: ids.map((id) => store.groups.find((group) => group.id === id)).filter((group) => !!group) }));
}

// Moves a group to index (counted before the move, like moveTab).
export function moveGroup(id: string, index: number) {
  update((store) => {
    const from = store.groups.findIndex((group) => group.id === id);
    if (from < 0) return store;
    const groups = store.groups.filter((group) => group.id !== id);
    groups.splice(Math.max(0, Math.min(groups.length, index > from ? index - 1 : index)), 0, store.groups[from]);
    return { ...store, groups };
  });
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
        ? withIds(group, [...group.ids, chatID])
        : group.ids.includes(chatID)
          ? withIds(group, group.ids.filter((x) => x !== chatID))
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
// The tab shown when nothing is selected: the last focused one if it is in ids, else the first.
export const shownPin = (ids: string[]) => {
  const last = lastPinnedChat();
  return last && ids.includes(last) ? last : ids[0];
};

export const usePinGroups = () => useSyncExternalStore(subscribe, () => store);

export function usePins(): Pins {
  return useSyncExternalStore(subscribe, activeGroup);
}
