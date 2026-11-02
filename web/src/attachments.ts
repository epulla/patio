import { api } from "./api";

const linkText = (text: string) => text.replace(/[[\]\\]/g, "\\$&");

export const imageTypes = ["image/png", "image/jpeg", "image/gif", "image/webp"];

const maxImageEdge = 2000;
const maxImageBytes = 5 << 20;
const imageExtensions: Record<string, string> = { "image/png": "png", "image/jpeg": "jpg", "image/webp": "webp" };

// Models shrink large images anyway (Claude caps them at 5 MB and ~1568 px), so shrink before upload.
// GIFs stay as they are to keep their animation.
export async function shrinkImage(file: File) {
  if (!imageTypes.includes(file.type) || file.type === "image/gif") return file;
  const bitmap = await createImageBitmap(file).catch(() => undefined);
  if (!bitmap) return file;
  const scale = Math.min(1, maxImageEdge / Math.max(bitmap.width, bitmap.height));
  if (scale === 1 && file.size <= maxImageBytes) {
    bitmap.close();
    return file;
  }
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(bitmap.width * scale);
  canvas.height = Math.round(bitmap.height * scale);
  const context = canvas.getContext("2d")!;
  context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  const encode = (type: string, quality?: number) => new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, quality));
  let blob = file.type === "image/jpeg" ? null : await encode(file.type);
  if (!blob || blob.size > maxImageBytes) {
    // JPEG has no transparency, so paint white behind it.
    context.globalCompositeOperation = "destination-over";
    context.fillStyle = "#fff";
    context.fillRect(0, 0, canvas.width, canvas.height);
    blob = await encode("image/jpeg", 0.85);
  }
  if (!blob) return file;
  const extension = imageExtensions[blob.type] ?? "png";
  return new File([blob], `${file.name.replace(/\.[^.]*$/, "")}.${extension}`, { type: blob.type });
}

// The Wails runtime's document-level drag listeners set dropEffect to "none" (file drop is off
// natively), which blocks the drop; claim file drags on our drop zones before they bubble there.
export function keepFileDrop(event: { stopPropagation(): void; dataTransfer: DataTransfer }) {
  event.stopPropagation();
  event.dataTransfer.dropEffect = "copy";
}

export function pastedFile(file: File) {
  if (!file.type.startsWith("image/") || (file.name && file.name !== "image.png")) return file;
  const now = new Date();
  const pad = (value: number) => String(value).padStart(2, "0");
  const stamp = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}.${pad(now.getMinutes())}.${pad(now.getSeconds())}`;
  const extension = file.type.split("/")[1]?.replace("jpeg", "jpg") || "png";
  return new File([file], `pasted-${stamp}.${extension}`, { type: file.type });
}

export type TicketAttachment = {
  name: string;
  original_name: string;
  kind: string;
  mime: string;
  size: number;
  created_at: string;
  path: string;
};

export const maxTicketAttachments = 20;

// Parentheses are encoded too: an unmatched one would end the Markdown link destination early.
export const ticketAttachmentURL = (ticketId: string, name: string) =>
  `/api/tickets/${ticketId}/attachments/${encodeURIComponent(name).replace(/[()]/g, (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`)}`;

// Matches the URLs above; Markdown only renders <img> for these.
export const ticketAttachmentPath = /^\/api\/tickets\/[^/]+\/attachments\/[^/]+$/;

// Returns a problem key for files the server would reject anyway, so they fail before upload.
export function ticketFileProblem(file: File): "imageLarge" | "fileLarge" | "" {
  const image = imageTypes.includes(file.type);
  if (image && file.size > 10 << 20) return "imageLarge";
  if (!image && file.size > 20 << 20) return "fileLarge";
  return "";
}

export async function uploadTicketFile(ticketId: string, file: File) {
  const attachment = await api.upload<TicketAttachment>(`/api/tickets/${ticketId}/attachments`, file);
  const link = `[${linkText(attachment.original_name || attachment.name)}](${ticketAttachmentURL(ticketId, attachment.name)})`;
  return attachment.kind === "image" ? `!${link}` : link;
}

// Placeholder for a file that is still uploading or waits for its ticket to exist.
export const pendingLink = (id: string, label: string) => `![${linkText(label)}](pending:${id})`;
const pendingPattern = (id: string) => new RegExp(`!\\[(?:[^\\]\\\\]|\\\\.)*\\]\\(pending:${id}\\)`);
export const hasPending = (text: string, id: string) => pendingPattern(id).test(text);
export const replacePending = (text: string, id: string, replacement: string) =>
  text.replace(pendingPattern(id), () => replacement);

export type TicketField = "description" | "comment";
export const commentDraftKey = (ticketId: string) => `ticket-comment-draft:${ticketId}`;

type Swap = (id: string, replacement: string) => void;
const editors = new Map<string, Swap>();

// The editor open now for a ticket's text registers here, so an upload started in an earlier editor
// (the user left the ticket and came back) swaps its placeholder in the current text, never in old text.
export function registerEditor(ticketId: string, field: TicketField, swap: Swap) {
  const key = `${ticketId}:${field}`;
  editors.set(key, swap);
  return () => {
    if (editors.get(key) === swap) editors.delete(key);
  };
}

// Without an open editor, swap the placeholder where the text is kept. A placeholder the user
// deleted is left alone; the uploaded file stays in the ticket's attachments.
export async function finishPending(ticketId: string, field: TicketField, id: string, replacement: string) {
  const editor = editors.get(`${ticketId}:${field}`);
  if (editor) return editor(id, replacement);
  if (field === "comment") {
    const draft = localStorage.getItem(commentDraftKey(ticketId));
    if (draft && hasPending(draft, id)) localStorage.setItem(commentDraftKey(ticketId), replacePending(draft, id, replacement));
    return;
  }
  const { description } = await api.get<{ description: string }>(`/api/tickets/${ticketId}`);
  if (hasPending(description, id)) await api.put(`/api/tickets/${ticketId}`, { description: replacePending(description, id, replacement) });
}
