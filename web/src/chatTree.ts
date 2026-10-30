import type { Chat } from "./api";
import type { ChatStatus } from "./chatStatus";

export type TreeRow = { chat: Chat; depth: number; children: number; state?: ChatStatus };

const openKey = "chat-tree-open";

export function loadTreeOpen() {
  try {
    return new Set<string>(JSON.parse(localStorage.getItem(openKey) ?? "[]"));
  } catch {
    return new Set<string>();
  }
}

export function saveTreeOpen(open: Set<string>) {
  localStorage.setItem(openKey, JSON.stringify([...open]));
}

export function chatAncestors(chats: Chat[], id: string) {
  const parents = new Map(chats.map((chat) => [chat.id, chat.parent_chat_id]));
  const found: string[] = [];
  for (let parent = parents.get(id); parent && parents.has(parent) && !found.includes(parent); parent = parents.get(parent))
    found.push(parent);
  return found;
}

const rank: Record<ChatStatus, number> = { reply: 1, working: 2, approval: 3 };

// Handoff chats nest under their parent, newest first; a child whose parent isn't loaded stays top-level.
// Parents sort by their newest descendant and show working/approval while any descendant has it.
export function chatTree(chats: Chat[], status: Record<string, ChatStatus | undefined>, open: Set<string>): TreeRow[] {
  const loaded = new Set(chats.map((chat) => chat.id));
  const kids = new Map<string, Chat[]>();
  const roots: Chat[] = [];
  for (const chat of chats) {
    const parent = chat.parent_chat_id;
    if (parent && parent !== chat.id && loaded.has(parent)) kids.set(parent, [...(kids.get(parent) ?? []), chat]);
    else roots.push(chat);
  }
  const summaries = new Map<string, { at: string; state?: ChatStatus }>();
  function summary(chat: Chat) {
    let result = summaries.get(chat.id);
    if (result) return result;
    result = { at: chat.updated_at, state: status[chat.id] };
    for (const child of kids.get(chat.id) ?? []) {
      const { at, state } = summary(child);
      if (at > result.at) result.at = at;
      if (state && state !== "reply" && (!result.state || rank[state] > rank[result.state])) result.state = state;
    }
    summaries.set(chat.id, result);
    return result;
  }
  const rows: TreeRow[] = [];
  function walk(chat: Chat, depth: number) {
    const children = kids.get(chat.id) ?? [];
    rows.push({ chat, depth, children: children.length, state: summary(chat).state });
    if (open.has(chat.id)) children.forEach((child) => walk(child, depth + 1));
  }
  [...roots].sort((a, b) => summary(b).at.localeCompare(summary(a).at)).forEach((chat) => walk(chat, 0));
  return rows;
}
