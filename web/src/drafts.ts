import { api, type ChatAttachment } from "./api";

export type Draft = { text: string; attachments: ChatAttachment[] };

const legacyPrefix = "chat-draft:";

// Saves per chat run in order, so a send's clear can't be overtaken by an earlier keystroke's save.
const saving = new Map<string, Promise<unknown>>();

export function saveDraft(chatID: string, text: string, attachments: ChatAttachment[]) {
  const next = (saving.get(chatID) ?? Promise.resolve())
    .catch(() => undefined)
    .then(() => api.put(`/api/chats/${chatID}/draft`, { text, attachments }));
  saving.set(chatID, next);
  return next;
}

// Drafts used to live in localStorage (plain text or JSON); the first load moves one into the DB.
function legacyDraft(chatID: string): Draft | undefined {
  const raw = localStorage.getItem(legacyPrefix + chatID);
  if (raw === null) return undefined;
  try {
    const value = JSON.parse(raw);
    if (value && typeof value === "object" && typeof value.text === "string" && Array.isArray(value.attachments))
      return { text: value.text, attachments: value.attachments };
  } catch {
    // plain text draft
  }
  return { text: raw, attachments: [] };
}

export async function loadDraft(chatID: string): Promise<Draft> {
  await saving.get(chatID)?.catch(() => undefined);
  const stored = await api.get<Draft>(`/api/chats/${chatID}/draft`);
  const legacy = legacyDraft(chatID);
  if (!legacy) return stored;
  if (stored.text || stored.attachments.length) {
    localStorage.removeItem(legacyPrefix + chatID);
    return stored;
  }
  await saveDraft(chatID, legacy.text, legacy.attachments);
  localStorage.removeItem(legacyPrefix + chatID);
  return legacy;
}

// Drafts being sent: cleared from storage when the send starts, restored if it fails.
// One send per chat at a time, also across remounted composers, so each chat has one copy.
const sending = new Map<string, Draft>();
const listeners = new Map<string, Set<(draft: Draft) => void>>();

export function isSending(chatID: string) {
  return sending.has(chatID);
}

export function takeDraft(chatID: string, text: string, attachments: ChatAttachment[]) {
  sending.set(chatID, { text, attachments });
  return saveDraft(chatID, "", []);
}

export function finishSend(chatID: string) {
  sending.delete(chatID);
}

// Puts a failed send back: mounted composers take it if they're still empty, otherwise the stored
// draft gets it unless the chat has a newer one. Callers hold isSending until this settles.
export async function restoreDraft(chatID: string) {
  const draft = sending.get(chatID);
  if (!draft) return;
  const mounted = listeners.get(chatID);
  if (mounted?.size) return mounted.forEach((listener) => listener(draft));
  const stored = await loadDraft(chatID);
  if (stored.text || stored.attachments.length) return;
  await saveDraft(chatID, draft.text, draft.attachments);
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
