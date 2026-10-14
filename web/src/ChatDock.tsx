import { useEffect, useState } from "react";
import { api, type Board, type Chat, type Connector, type Ticket } from "./api";
import { navigate } from "./App";
import { Conversation, type PageContext } from "./Chats";
import { Icon, IconButton, type IconName } from "./icons";
import { pinAriaShortcut, pinNumber, pinShortcut } from "./pins";

export const dockShortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘J" : "Ctrl+J";
export type ChatStatus = "working" | "approval" | "reply";
export const statusIcons: Record<ChatStatus, IconName> = { working: "dot", approval: "hand", reply: "check" };
const statusLabels: Record<ChatStatus, string> = { working: "working", approval: "waiting for approval", reply: "new reply" };
const minWidth = 320;
const maxWidth = 720;
const defaultWidth = 400;
const linkText = (text: string) => text.replace(/[[\]\\]/g, "\\$&");
const clampWidth = (width: number) => Math.min(maxWidth, Math.max(minWidth, Math.round(width)));

export function usePinnedChats(ids: string[], refresh: number) {
  const [chats, setChats] = useState<Chat[]>([]);
  const [status, setStatus] = useState<Record<string, ChatStatus | undefined>>({});
  const key = ids.join();

  useEffect(() => {
    if (!key) return setChats([]);
    // burnedout: assumed pinned chats are among the 200 most recently updated
    api
      .get<{ chats: Chat[] }>("/api/chats?limit=200")
      .then((page) => {
        const pinned = page.chats.filter((chat) => key.split(",").includes(chat.id));
        setChats(pinned);
        setStatus((current) =>
          Object.fromEntries(
            pinned.map((chat) => [
              chat.id,
              chat.pending_count ? "approval" : chat.running ? "working" : current[chat.id] === "reply" ? "reply" : undefined,
            ]),
          ),
        );
      })
      .catch(() => undefined);
  }, [key, refresh]);

  useEffect(() => {
    if (!key) return;
    const unsubscribes = key.split(",").map((id) =>
      window.runtime?.EventsOn(`chat:${id}`, (data) => {
        const type = (data as { type: string }).type;
        const next = type === "approval" ? "approval" : type === "done" ? "reply" : type === "error" ? undefined : "working";
        setStatus((current) => ({ ...current, [id]: next }));
      }),
    );
    return () => unsubscribes.forEach((unsubscribe) => unsubscribe?.());
  }, [key]);

  const seen = (id: string) => setStatus((current) => ({ ...current, [id]: undefined }));
  return { chats, status, seen };
}

export default function ChatDock({
  ids,
  chats,
  status,
  refresh,
  page,
  onSeen,
  onClose,
}: {
  ids: string[];
  chats: Chat[];
  status: Record<string, ChatStatus | undefined>;
  refresh: number;
  page?: { kind: "ticket" | "board"; id: string };
  onSeen: (id: string) => void;
  onClose: () => void;
}) {
  const [active, setActive] = useState(ids[0]);
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [width, setWidth] = useState(() => clampWidth(Number(localStorage.getItem("chat-dock-width")) || defaultWidth));
  const [focusRequest, setFocusRequest] = useState<string>();
  const [loaded, setLoaded] = useState<{ key: string; context: PageContext }>();
  const current = ids.includes(active) ? active : ids[0];
  const pageKey = page && `${page.kind}:${page.id}`;
  const context = loaded && loaded.key === pageKey ? loaded.context : undefined;

  useEffect(() => {
    if (!page || !pageKey) return;
    let live = true;
    const load: Promise<PageContext> =
      page.kind === "ticket"
        ? api.get<Ticket>(`/api/tickets/${page.id}`).then((ticket) => ({
            icon: "ticket",
            label: ticket.code,
            line: `Viewing ticket [${linkText(`${ticket.code}: ${ticket.title}`)}](/tickets/${ticket.id})`,
          }))
        : api.get<Board>(`/api/boards/${page.id}`).then((board) => ({
            icon: "boards",
            label: board.name,
            line: `Viewing board [${linkText(board.name)}](/boards/${board.id})`,
          }));
    load.then((context) => live && setLoaded({ key: pageKey, context })).catch(() => undefined);
    return () => {
      live = false;
    };
  }, [pageKey, refresh]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const n = pinNumber(event, ids.length);
      if (!n) return;
      event.preventDefault();
      const target = ids[n - 1];
      if (target === current) document.querySelector<HTMLTextAreaElement>(".chat-dock textarea")?.focus();
      setActive(target);
      setFocusRequest(target);
    };
    addEventListener("keydown", onKeyDown);
    return () => removeEventListener("keydown", onKeyDown);
  }, [ids, current]);

  function resize(next: number) {
    const clamped = clampWidth(next);
    localStorage.setItem("chat-dock-width", String(clamped));
    setWidth(clamped);
  }

  useEffect(() => {
    if (status[current] === "reply") onSeen(current);
  }, [current, status[current]]);

  useEffect(() => {
    api
      .get<Connector[]>("/api/connectors")
      .then(setConnectors)
      .catch(() => undefined);
  }, [refresh]);

  return (
    <aside className="chat-dock" aria-label="Pinned chats" style={{ width }}>
      <div
        role="separator"
        tabIndex={0}
        aria-label="Resize pinned chats"
        aria-orientation="vertical"
        aria-valuemin={minWidth}
        aria-valuemax={maxWidth}
        aria-valuenow={width}
        className="splitter dock-resize"
        onPointerDown={(event) => {
          event.preventDefault();
          const element = event.currentTarget;
          element.focus();
          const start = width;
          const origin = event.clientX;
          element.setPointerCapture(event.pointerId);
          element.onpointermove = (moved) => resize(start + origin - moved.clientX);
          element.onlostpointercapture = () => (element.onpointermove = null);
        }}
        onDoubleClick={() => resize(defaultWidth)}
        onKeyDown={(event) => {
          const step = ({ ArrowLeft: 24, ArrowRight: -24 } as Record<string, number>)[event.key];
          if (!step) return;
          event.preventDefault();
          resize(width + step);
        }}
      />
      <div className="dock-head">
        <div className="dock-chips">
          {ids.map((id, index) => {
            const title = chats.find((chat) => chat.id === id)?.title ?? "Chat";
            const state = status[id];
            return (
              <button
                key={id}
                className="dock-chip"
                aria-pressed={id === current}
                aria-label={state ? `${title}, ${statusLabels[state]}` : title}
                aria-keyshortcuts={pinAriaShortcut(index + 1)}
                onClick={() => setActive(id)}
              >
                <kbd className="pin-kbd" aria-hidden="true">
                  {pinShortcut(index + 1)}
                </kbd>
                {state && (
                  <span className="dock-dot" data-state={state} aria-hidden="true">
                    <Icon name={statusIcons[state]} />
                  </span>
                )}
                <span className="dock-title">{title}</span>
              </button>
            );
          })}
        </div>
        <IconButton className="ghost" icon="externalLink" label="Open in Chats" onClick={() => navigate(`/ai/chats/${current}`)} />
        <kbd className="dock-kbd" title={`Toggle pinned chats with ${dockShortcut}`}>
          {dockShortcut}
        </kbd>
        <IconButton className="ghost" icon="chevronsRight" label={`Close dock (${dockShortcut})`} onClick={onClose} />
      </div>
      <Conversation
        key={current}
        id={current}
        refresh={refresh}
        connectors={connectors}
        compact
        focused={focusRequest === current}
        context={context}
        otherWindow={chats.find((chat) => chat.id === current)?.other_window}
        onChanged={() => undefined}
      />
    </aside>
  );
}
