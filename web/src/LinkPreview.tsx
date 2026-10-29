import { useEffect, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { api, type BoardData, type Bot, type Chat, type ChatMessage, type Ticket } from "./api";
import DueBadge from "./DueBadge";
import { useText } from "./i18n";

export const previewRoute = /^\/(tickets|boards|ai\/chats)\/([^/]+)$/;

type Preview =
  | { kind: "ticket"; ticket: Ticket; board: BoardData }
  | { kind: "board"; board: BoardData }
  | { kind: "chat"; chat: Chat; bot?: Bot; last?: ChatMessage };

const cache = new Map<string, Promise<Preview>>();

async function load(href: string): Promise<Preview> {
  const [, kind, id] = href.match(previewRoute)!;
  if (kind === "tickets") {
    const ticket = await api.get<Ticket>(`/api/tickets/${id}`);
    return { kind: "ticket", ticket, board: await api.get<BoardData>(`/api/boards/${ticket.board_id}/data`) };
  }
  if (kind === "boards") return { kind: "board", board: await api.get<BoardData>(`/api/boards/${id}/data`) };
  const [{ chat, messages }, bots] = await Promise.all([
    api.get<{ chat: Chat; messages: ChatMessage[] }>(`/api/chats/${id}`),
    api.get<Bot[]>("/api/bots"),
  ]);
  return {
    kind: "chat",
    chat,
    bot: bots.find((bot) => bot.id === chat.bot_id),
    last: [...messages].reverse().find((message) => message.role === "user" || message.role === "assistant"),
  };
}

const plain = (markdown: string) => markdown.replace(/\[([^\]]*)\]\([^)]*\)/g, "$1").replace(/[*_`#>]/g, "").trim();

export default function LinkPreview({
  href,
  anchor,
  onEnter,
  onLeave,
}: {
  href: string;
  anchor: DOMRect;
  onEnter: () => void;
  onLeave: () => void;
}) {
  const t = useText({
    en: { running: "Running", idle: "Idle", tickets: (n: number) => `${n} ticket${n === 1 ? "" : "s"}` },
    es: { running: "En curso", idle: "Inactivo", tickets: (n: number) => `${n} ticket${n === 1 ? "" : "s"}` },
  });
  const [preview, setPreview] = useState<Preview>();
  useEffect(() => {
    let request = cache.get(href);
    if (!request) {
      request = load(href);
      cache.set(href, request);
    }
    let live = true;
    request.then((value) => live && setPreview(value), () => undefined);
    return () => {
      live = false;
    };
  }, [href]);
  if (!preview) return null;
  const below = anchor.bottom + 220 < innerHeight;
  const style: CSSProperties = {
    left: Math.max(8, Math.min(anchor.left, innerWidth - 328)),
    ...(below ? { top: anchor.bottom + 6 } : { bottom: innerHeight - anchor.top + 6 }),
  };
  let body;
  if (preview.kind === "ticket") {
    const { ticket, board } = preview;
    const type = board.ticket_types.find((item) => item.id === ticket.ticket_type_id);
    body = (
      <>
        <div className="ticket-meta">
          <span className="ticket-code">{ticket.code}</span>
          {type && (
            <span className="type" style={{ "--type-color": type.color } as CSSProperties}>
              {type.name}
            </span>
          )}
        </div>
        <strong>{ticket.title}</strong>
        <div className="link-preview-row">
          <span>{board.statuses.find((status) => status.id === ticket.status_id)?.name}</span>
          {ticket.due_date && <DueBadge date={ticket.due_date} />}
        </div>
        {ticket.description && <p className="link-preview-text">{plain(ticket.description)}</p>}
      </>
    );
  } else if (preview.kind === "board") {
    const { board, statuses } = preview.board;
    body = (
      <>
        <strong>{board.name}</strong>
        <ul className="link-preview-list">
          {statuses.map((status) => (
            <li key={status.id}>
              <span>{status.name}</span>
              <span>{t.tickets(status.tickets.length)}</span>
            </li>
          ))}
        </ul>
      </>
    );
  } else {
    const { chat, bot, last } = preview;
    body = (
      <>
        <strong>{chat.title}</strong>
        <div className="link-preview-row">
          {bot && <span>{bot.name}</span>}
          <span>{chat.running ? t.running : t.idle}</span>
        </div>
        {last && <p className="link-preview-text">{plain(last.content)}</p>}
      </>
    );
  }
  return createPortal(
    <div className="link-preview" style={style} onMouseEnter={onEnter} onMouseLeave={onLeave}>
      {body}
    </div>,
    document.body,
  );
}
