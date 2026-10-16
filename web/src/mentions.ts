import { api, type SearchTicket, type User } from "./api";

export type Trigger = { char: "@" | "#"; start: number; query: string };
export type Suggestion = { key: string; label: string; detail?: string; insert: string };

// A trigger only counts at the start of the text or after whitespace, so emails and `# Heading` don't open it.
export function findTrigger(value: string, caret: number): Trigger | null {
  const match = /(?:^|\s)([@#])([^\s@#]*)$/.exec(value.slice(0, caret));
  if (!match) return null;
  const char = match[1] as Trigger["char"];
  const query = match[2];
  if (char === "#" && !query) return null;
  return { char, start: caret - query.length - 1, query };
}

let users: Promise<User[]> | null = null;

export async function suggest({ char, query }: Trigger): Promise<Suggestion[]> {
  const q = query.toLowerCase();
  if (char === "@") {
    users ??= api.get<User[]>("/api/users").catch((error) => {
      users = null;
      throw error;
    });
    return (await users)
      .filter((user) => user.name.toLowerCase().startsWith(q))
      .map((user) => ({ key: user.id, label: user.name, detail: user.is_ai ? "AI" : undefined, insert: `@${user.name}` }));
  }
  // Debounce: callers drop results that arrive after a newer request.
  await new Promise((resolve) => setTimeout(resolve, 150));
  const tickets = await api.get<SearchTicket[]>(`/api/tickets/search?q=${encodeURIComponent(query)}&sort=updated`);
  return tickets.slice(0, 8).map((ticket) => ({
    key: ticket.id,
    label: `${ticket.code} · ${ticket.title}`,
    detail: ticket.status_name,
    insert: `[${`${ticket.code}: ${ticket.title}`.replace(/[[\]\\]/g, "\\$&")}](/tickets/${ticket.id})`,
  }));
}

const mirrored = [
  "boxSizing",
  "width",
  "paddingTop",
  "paddingRight",
  "paddingBottom",
  "paddingLeft",
  "borderTopWidth",
  "borderRightWidth",
  "borderBottomWidth",
  "borderLeftWidth",
  "fontFamily",
  "fontSize",
  "fontWeight",
  "fontStyle",
  "letterSpacing",
  "lineHeight",
  "textTransform",
  "wordSpacing",
  "tabSize",
] as const;

// Position of the caret below the given index, relative to the textarea's offset parent.
export function caretPosition(el: HTMLTextAreaElement, index: number) {
  const style = getComputedStyle(el);
  const div = document.createElement("div");
  for (const prop of mirrored) div.style[prop] = style[prop];
  Object.assign(div.style, { position: "absolute", visibility: "hidden", whiteSpace: "pre-wrap", overflowWrap: "break-word" });
  div.textContent = el.value.slice(0, index);
  const marker = document.createElement("span");
  marker.textContent = "\u200b";
  div.append(marker);
  document.body.append(div);
  const top = el.offsetTop + marker.offsetTop + marker.offsetHeight - el.scrollTop;
  const left = el.offsetLeft + Math.min(marker.offsetLeft, el.clientWidth - 200);
  div.remove();
  return { top, left: Math.max(el.offsetLeft, left) };
}
