// Adds a chat link to a ticket description's first links line (`**Repo:** … · **Chat:** …`),
// the same format the ticket-chat-link skill uses.
export function addChatLink(description: string, title: string, chatID: string) {
  const href = `/ai/chats/${chatID}`;
  if (description.includes(`(${href})`)) return description;
  const link = markdownLink(title, href);
  const newline = description.indexOf("\n");
  const first = newline < 0 ? description : description.slice(0, newline);
  const rest = newline < 0 ? "" : description.slice(newline);
  if (!/^\*\*[^*\n]+:\*\*/.test(first)) return `**Chat:** ${link}${description ? `\n\n${description}` : ""}`;
  const parts = first.split(" · ");
  const chat = parts.findIndex((part) => part.startsWith("**Chat:**"));
  if (chat >= 0) parts[chat] += `, ${link}`;
  else parts.splice(1, 0, `**Chat:** ${link}`);
  return parts.join(" · ") + rest;
}

export const markdownLink = (text: string, href: string) => `[${text.replace(/[[\]\\]/g, "\\$&")}](${href})`;
