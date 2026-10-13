import { useEffect, useState } from "react";
import { api, type Chat, type Connector } from "./api";
import { navigate } from "./App";
import { Conversation } from "./Chats";
import { IconButton } from "./icons";

export type ChatStatus = "working" | "approval" | "reply";
export const statusMarks: Record<ChatStatus, string> = { working: "●", approval: "✋", reply: "✓" };
const statusLabels: Record<ChatStatus, string> = { working: "working", approval: "waiting for approval", reply: "new reply" };

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
  onSeen,
  onClose,
}: {
  ids: string[];
  chats: Chat[];
  status: Record<string, ChatStatus | undefined>;
  refresh: number;
  onSeen: (id: string) => void;
  onClose: () => void;
}) {
  const [active, setActive] = useState(ids[0]);
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const current = ids.includes(active) ? active : ids[0];

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
    <aside className="chat-dock" aria-label="Pinned chats">
      <div className="dock-head">
        <div className="dock-chips">
          {ids.map((id) => {
            const title = chats.find((chat) => chat.id === id)?.title ?? "Chat";
            const state = status[id];
            return (
              <button
                key={id}
                className="dock-chip"
                aria-pressed={id === current}
                aria-label={state ? `${title}, ${statusLabels[state]}` : title}
                onClick={() => setActive(id)}
              >
                {state && (
                  <span className={`dock-dot ${state}`} aria-hidden="true">
                    {statusMarks[state]}
                  </span>
                )}
                {title}
              </button>
            );
          })}
        </div>
        <IconButton className="ghost" icon="externalLink" label="Open in Chats" onClick={() => navigate(`/ai/chats/${current}`)} />
        <IconButton className="ghost" icon="chevronsRight" label="Close dock" onClick={onClose} />
      </div>
      <Conversation
        key={current}
        id={current}
        refresh={refresh}
        connectors={connectors}
        compact
        otherWindow={chats.find((chat) => chat.id === current)?.other_window}
        onChanged={() => undefined}
      />
    </aside>
  );
}
