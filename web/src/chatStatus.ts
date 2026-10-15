import { useEffect, useSyncExternalStore } from "react";
import type { Chat } from "./api";
import type { IconName } from "./icons";

export type ChatStatus = "working" | "approval" | "reply";
export const statusIcons: Record<ChatStatus, IconName> = { working: "dot", approval: "hand", reply: "check" };
export const statusLabels: Record<ChatStatus, string> = { working: "working", approval: "waiting for approval", reply: "new reply" };

let current: Record<string, ChatStatus | undefined> = {};
const listeners = new Set<() => void>();

function update(next: Record<string, ChatStatus | undefined>) {
  if (Object.entries(next).every(([id, state]) => current[id] === state)) return;
  current = { ...current, ...next };
  listeners.forEach((listener) => listener());
}

// Server state wins for working/approval; "reply" is client-only and survives until seen.
export function syncChatStatus(chats: Chat[]) {
  update(
    Object.fromEntries(
      chats.map((chat) => [
        chat.id,
        chat.pending_count ? "approval" : chat.running ? "working" : current[chat.id] === "reply" ? "reply" : undefined,
      ]),
    ),
  );
}

export function markSeen(id: string) {
  if (current[id] === "reply") update({ [id]: undefined });
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useChatStatus(ids: string[]) {
  const key = ids.join();
  useEffect(() => {
    if (!key) return;
    const unsubscribes = key.split(",").map((id) =>
      window.runtime?.EventsOn(`chat:${id}`, (data) => {
        const type = (data as { type: string }).type;
        update({ [id]: type === "approval" ? "approval" : type === "done" ? "reply" : type === "error" ? undefined : "working" });
      }),
    );
    return () => unsubscribes.forEach((unsubscribe) => unsubscribe?.());
  }, [key]);
  return useSyncExternalStore(subscribe, () => current);
}
