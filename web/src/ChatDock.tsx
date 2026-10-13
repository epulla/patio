import { useEffect, useState } from "react";
import { api, type Chat, type Connector } from "./api";
import { navigate } from "./App";
import { Conversation } from "./Chats";
import { IconButton } from "./icons";

export function usePinnedChats(ids: string[], refresh: number) {
  const [chats, setChats] = useState<Chat[]>([]);
  const key = ids.join();
  useEffect(() => {
    if (!key) return setChats([]);
    // burnedout: assumed pinned chats are among the 200 most recently updated
    api
      .get<{ chats: Chat[] }>("/api/chats?limit=200")
      .then((page) => setChats(page.chats.filter((chat) => key.split(",").includes(chat.id))))
      .catch(() => undefined);
  }, [key, refresh]);
  return chats;
}

export default function ChatDock({
  ids,
  chats,
  refresh,
  onClose,
}: {
  ids: string[];
  chats: Chat[];
  refresh: number;
  onClose: () => void;
}) {
  const [active, setActive] = useState(ids[0]);
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const current = ids.includes(active) ? active : ids[0];

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
          {ids.map((id) => (
            <button key={id} className="dock-chip" aria-pressed={id === current} onClick={() => setActive(id)}>
              {chats.find((chat) => chat.id === id)?.title ?? "Chat"}
            </button>
          ))}
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
