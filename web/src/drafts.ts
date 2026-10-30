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
