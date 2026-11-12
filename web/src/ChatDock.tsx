import { useEffect, useRef, useState } from "react";
import { api, type Board, type Chat, type Connector, type Ticket } from "./api";
import { navigate } from "./App";
import { Conversation, type PageContext } from "./Chats";
import { markSeen, syncChatStatus, useChatStatus, type ChatStatus } from "./chatStatus";
import { IconButton } from "./icons";
import PinTabs from "./PinTabs";
import { isUnpinKey, moveTab, stepIndex, tabMoveStep, tabStep, lastPinnedChat, rememberPinnedChat, pinNumber, pinStyle, unpin, usePinGroups } from "./pins";
import { useText } from "./i18n";
import { useSplitter } from "./splitter";

const text = {
  en: { pinnedChats: "Pinned chats", resizePinned: "Resize pinned chats", openChats: "Open in Chats", togglePinned: (shortcut: string) => `Toggle pinned chats with ${shortcut}`, closeDock: (shortcut: string) => `Close dock (${shortcut})`, emptyGroup: (name: string) => `${name} has no chats yet.` },
  es: { pinnedChats: "Chats fijados", resizePinned: "Cambiar tamaño de chats fijados", openChats: "Abrir en Chats", togglePinned: (shortcut: string) => `Alternar chats fijados con ${shortcut}`, closeDock: (shortcut: string) => `Cerrar panel (${shortcut})`, emptyGroup: (name: string) => `${name} aún no tiene chats.` },
};

export const dockShortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘J" : "Ctrl+J";
export const linkText = (text: string) => text.replace(/[[\]\\]/g, "\\$&");

export function usePinnedChats(ids: string[], refresh: number) {
  const [chats, setChats] = useState<Chat[]>([]);
  const status = useChatStatus(ids);
  const key = ids.join();

  useEffect(() => {
    if (!key) return setChats([]);
    let live = true;
    api
      .get<{ chats: Chat[] }>(`/api/chats?ids=${encodeURIComponent(key)}`)
      .then((page) => {
        if (!live) return;
        setChats(page.chats);
        syncChatStatus(page.chats);
      })
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [key, refresh]);

  return { chats, status, seen: markSeen };
}

export default function ChatDock({
  ids,
  chats,
  status,
  refresh,
  focus,
  page,
  onSeen,
  onClose,
}: {
  ids: string[];
  chats: Chat[];
  status: Record<string, ChatStatus | undefined>;
  refresh: number;
  focus?: { id: string };
  page?: { kind: "ticket" | "board"; id: string };
  onSeen: (id: string) => void;
  onClose: () => void;
}) {
  const t = useText(text);
  const groups = usePinGroups();
  const activeGroup = groups.groups.find((group) => group.id === groups.active);
  const groupName = activeGroup?.name ?? "";
  const [active, setActive] = useState(() => lastPinnedChat() ?? ids[0]);
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const splitter = useSplitter({ storageKey: "chat-dock-width", defaultWidth: 400, min: 320, max: 720, side: "left", label: t.resizePinned });
  const [focusRequest, setFocusRequest] = useState<string>();
  const [loaded, setLoaded] = useState<{ key: string; context: PageContext }>();
  const current = ids.includes(active) ? active : ids[0];
  useEffect(() => {
    if (current) rememberPinnedChat(current);
  }, [current]);
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
      if (isUnpinKey(event) && current) {
        event.preventDefault();
        return unpinChat(current);
      }
      const move = tabMoveStep(event);
      if (move && current && ids.includes(current)) {
        event.preventDefault();
        const index = ids.indexOf(current);
        return moveTab(current, move > 0 ? index + 2 : index - 1);
      }
      const step = tabStep(event);
      const n = step ? (ids.length ? stepIndex(ids.indexOf(current), step, ids.length) + 1 : 0) : pinNumber(event, ids.length);
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

  useEffect(() => {
    if (!focus) return;
    if (focus.id === current) document.querySelector<HTMLTextAreaElement>(".chat-dock textarea")?.focus();
    setActive(focus.id);
    setFocusRequest(focus.id);
  }, [focus]);

  function unpinChat(id: string) {
    const index = ids.indexOf(id);
    if (id === current) setActive(ids[index + 1] ?? ids[index - 1]);
    unpin(id);
  }

  useEffect(() => {
    if (status[current] === "reply") onSeen(current);
  }, [current, status[current]]);

  // Loaded on mount and after a connector edit; a failed load retries on the next refresh.
  const connectorsStale = useRef(true);
  const [connectorEdits, setConnectorEdits] = useState(0);
  useEffect(() => {
    const onChange = () => {
      connectorsStale.current = true;
      setConnectorEdits((value) => value + 1);
    };
    addEventListener("connectors-changed", onChange);
    return () => removeEventListener("connectors-changed", onChange);
  }, []);
  useEffect(() => {
    if (!connectorsStale.current) return;
    api
      .get<Connector[]>("/api/connectors")
      .then((list) => {
        connectorsStale.current = false;
        setConnectors(list);
      })
      .catch(() => undefined);
  }, [refresh, connectorEdits]);

  return (
    <aside className="chat-dock" aria-label={t.pinnedChats} style={{ width: splitter.width, ...(activeGroup && pinStyle(activeGroup.color)) }}>
      <div className="splitter dock-resize" {...splitter.props} />
      <PinTabs chats={chats} status={status} current={current} onSelect={setActive} onUnpin={unpinChat}>
        <IconButton className="ghost" icon="externalLink" label={t.openChats} onClick={() => navigate(current ? `/ai/chats/${current}` : "/ai/chats")} />
        <kbd className="dock-kbd" title={t.togglePinned(dockShortcut)}>
          {dockShortcut}
        </kbd>
        <IconButton className="ghost" icon="chevronsRight" label={t.closeDock(dockShortcut)} onClick={onClose} />
      </PinTabs>
      {current ? (
      <Conversation
        key={current}
        id={current}
        refresh={refresh}
        connectors={connectors}
        compact
        focused={focusRequest === current}
        context={context}
        onChanged={() => undefined}
      />
      ) : (
        <p className="muted dock-empty">{t.emptyGroup(groupName)}</p>
      )}
    </aside>
  );
}
