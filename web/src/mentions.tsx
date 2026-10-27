import { useRef, useState, type KeyboardEvent, type RefObject } from "react";
import { api, type SearchTicket, type User } from "./api";
import { useText } from "./i18n";

type Trigger = { char: "@" | "#"; start: number; query: string };
type Suggestion = { key: string; label: string; detail?: string; insert: string };

// A trigger only counts at the start of the text or after whitespace, so emails and `## Heading` don't open it.
function findTrigger(value: string, caret: number): Trigger | null {
  const match = /(?:^|\s)([@#])([^\s@#]*)$/.exec(value.slice(0, caret));
  if (!match) return null;
  return { char: match[1] as Trigger["char"], start: caret - match[2].length - 1, query: match[2] };
}

// Same rules as parseMentions in internal/api/mentions.go: case-insensitive, word boundaries, code ignored.
export function mentionsBot(text: string, name: string) {
  const mention = new RegExp(`(?<![\\p{L}\\p{N}_])@${name}(?![\\p{L}\\p{N}_])`, "iu");
  let fenced = false;
  return text.split("\n").some((line) => {
    if (/^[ \t]*```/.test(line)) fenced = !fenced;
    else if (!fenced) return mention.test(line.replace(/(?<!`)(`+)(?!`)[^]*?(?<!`)\1(?!`)/g, " "));
    return false;
  });
}

let users: Promise<User[]> | null = null;
// Called after bots change so @ suggestions pick up new names.
export const forgetMentionUsers = () => (users = null);

async function suggest({ char, query }: Trigger, hiddenBotId?: string): Promise<Suggestion[]> {
  const q = query.toLowerCase();
  if (char === "@") {
    users ??= api.get<User[]>("/api/users").catch((error) => {
      users = null;
      throw error;
    });
    return (await users)
      .filter((user) => user.is_ai && user.id !== hiddenBotId && user.name.toLowerCase().startsWith(q))
      .map((user) => ({ key: user.id, label: user.name, insert: `@${user.name}` }));
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
function caretPosition(el: HTMLTextAreaElement, index: number) {
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

type State = { trigger: Trigger; items: Suggestion[]; active: number; top?: number; left?: number };

// `@` bot and `#` ticket suggestions for a textarea. "caret" opens below the caret
// (the textarea's offset parent must be positioned); "above" leaves placement to CSS.
// hiddenBotId hides a chat's own bot, since mentioning it never opens a chat.
export function useMentions(
  ref: RefObject<HTMLTextAreaElement | null>,
  onChange: (value: string) => void,
  placement: "caret" | "above" = "caret",
  hiddenBotId?: string,
) {
  const t = useText({ en: { suggestions: "Suggestions" }, es: { suggestions: "Sugerencias" } });
  const [state, setState] = useState<State | null>(null);
  const request = useRef(0);

  async function update() {
    const el = ref.current;
    const id = ++request.current;
    const trigger = el && findTrigger(el.value, el.selectionStart);
    if (!trigger) return setState(null);
    const items = await suggest(trigger, hiddenBotId).catch(() => []);
    if (id !== request.current || !ref.current) return;
    if (!items.length) return setState(null);
    setState({ trigger, items, active: 0, ...(placement === "caret" ? caretPosition(ref.current, trigger.start) : {}) });
  }

  function close() {
    request.current++;
    setState(null);
  }

  function pick(item: Suggestion) {
    const el = ref.current;
    if (!el || !state) return;
    const { start } = state.trigger;
    const text = item.insert + " ";
    const caret = start + text.length;
    close();
    onChange(el.value.slice(0, start) + text + el.value.slice(el.selectionStart));
    requestAnimationFrame(() => {
      ref.current?.focus();
      ref.current?.setSelectionRange(caret, caret);
    });
  }

  // Returns true when the key was handled by the open list.
  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (!state || event.nativeEvent.isComposing) return false;
    const { items, active } = state;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      const step = event.key === "ArrowDown" ? 1 : -1;
      setState({ ...state, active: (active + step + items.length) % items.length });
    } else if (event.key === "Enter" || event.key === "Tab") pick(items[active]);
    else if (event.key === "Escape") close();
    else return false;
    event.preventDefault();
    event.stopPropagation();
    return true;
  }

  const popup = state && (
    <ul
      className="md-mentions"
      data-placement={placement}
      role="listbox"
      aria-label={t.suggestions}
      style={placement === "caret" ? { top: state.top, left: state.left } : undefined}
    >
      {state.items.map((item, index) => (
        <li
          key={item.key}
          role="option"
          aria-selected={index === state.active}
          onMouseDown={(event) => {
            event.preventDefault();
            pick(item);
          }}
          onMouseEnter={() => setState({ ...state, active: index })}
        >
          <span>{item.label}</span>
          {item.detail && <small>{item.detail}</small>}
        </li>
      ))}
    </ul>
  );

  return { update, close, onKeyDown, popup, open: !!state };
}
