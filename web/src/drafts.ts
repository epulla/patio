import type { ChatAttachment } from "./api";

export type Draft = { text: string; attachments: ChatAttachment[]; at: number };

const prefix = "chat-draft:";
const maxAge = 7 * 24 * 60 * 60 * 1000;

// Older drafts were stored as the plain text, and new-chat prefills still are.
function parse(raw: string | null): Draft | undefined {
  if (raw === null) return undefined;
  try {
    const value = JSON.parse(raw);
    if (value && typeof value === "object" && typeof value.text === "string" && Array.isArray(value.attachments))
      return value;
  } catch {
    // plain text draft
  }
  return { text: raw, attachments: [], at: Date.now() };
}

export function loadDraft(chatID: string): Draft {
  return parse(localStorage.getItem(prefix + chatID)) ?? { text: "", attachments: [], at: Date.now() };
}

export function saveDraft(chatID: string, text: string, attachments: ChatAttachment[]) {
  if (!text && !attachments.length) return localStorage.removeItem(prefix + chatID);
  localStorage.setItem(prefix + chatID, JSON.stringify({ text, attachments, at: Date.now() }));
}

// Drafts being sent: cleared from storage when the send starts, restored if it fails.
const sending = new Map<string, Draft>();
const listeners = new Map<string, Set<(draft: Draft) => void>>();

export function takeDraft(chatID: string, text: string, attachments: ChatAttachment[]) {
  sending.set(chatID, { text, attachments, at: Date.now() });
  saveDraft(chatID, "", []);
}

export function dropSending(chatID: string) {
  sending.delete(chatID);
}

// Puts a failed send back unless the chat has a newer draft; mounted composers get it via onRestore.
export function restoreDraft(chatID: string) {
  const draft = sending.get(chatID);
  sending.delete(chatID);
  const stored = loadDraft(chatID);
  if (!draft || stored.text || stored.attachments.length) return;
  saveDraft(chatID, draft.text, draft.attachments);
  listeners.get(chatID)?.forEach((listener) => listener(draft));
}

export function onRestore(chatID: string, listener: (draft: Draft) => void) {
  const set = listeners.get(chatID) ?? new Set();
  set.add(listener);
  listeners.set(chatID, set);
  return () => {
    set.delete(listener);
    if (!set.size) listeners.delete(chatID);
  };
}

export function pruneDrafts() {
  const now = Date.now();
  for (const key of Object.keys(localStorage)) {
    if (!key.startsWith(prefix)) continue;
    const raw = localStorage.getItem(key);
    const draft = parse(raw);
    if (!draft) continue;
    if (now - draft.at > maxAge) localStorage.removeItem(key);
    // Stamp plain text drafts so they age out too.
    else if (raw !== JSON.stringify(draft)) saveDraft(key.slice(prefix.length), draft.text, draft.attachments);
  }
}
