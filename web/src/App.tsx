import { useEffect, useLayoutEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import BoardList from "./BoardList";
import BoardView, { NewTicketPage } from "./BoardView";
import TicketDetail from "./TicketDetail";
import PrintTicket from "./PrintTicket";
import Calendar from "./Calendar";
import Connectors from "./Connectors";
import Chats, { createChatWithDefaults, NewChatDialog } from "./Chats";
import { isShortcutsKey, ShortcutsDialog } from "./Shortcuts";
import ChatDock, { dockShortcut, usePinnedChats } from "./ChatDock";
import { statusIcons } from "./chatStatus";
import { cycleGroup, groupOf, groupStep, isNewPinnedKey, lastPinnedChat, maxPins, pin, rememberPinnedChat, pruneSplit, setActiveGroup, shownPin, splitLeaves, usePinGroups, usePins } from "./pins";
import McpServers from "./McpServers";
import Skills from "./Skills";
import ScheduledTasks from "./ScheduledTasks";
import Monitors from "./Monitors";
import Settings from "./Settings";
import Onboarding from "./Onboarding";
import Tour from "./Tour";
import { confirmDialog } from "./confirm";
import Tickets from "./Tickets";
import Notifications, { NotificationsPage } from "./Notifications";
import { api, type Board, type Chat, type Config, type Connector } from "./api";
export type { Config } from "./api";
import { ThemeToggle } from "./theme";
import { Icon, IconButton } from "./icons";
import { lang, locale, useText } from "./i18n";

const text = {
  en: {
    never: "Never", comingSoon: "Coming soon.", expand: "Expand sidebar", collapse: "Collapse sidebar",
    boards: "Boards", tickets: "Tickets", calendar: "Calendar", connectors: "Connectors", chats: "Chats",
    skills: "Skills", scheduled: "Scheduled tasks", yourBoards: "Your boards", settings: "Settings",
    monitors: "Monitors", monitorChanges: (n: number) => `, ${n} unread change${n === 1 ? "" : "s"}`,
    pinned: (n: number) => `Pinned chats (${n})`, approval: ", approval needed", reply: ", new reply",
    pinnedTip: (n: number, shortcut: string) => `Pinned chats (${n}) ${shortcut}`,
    groupFull: (name: string) => `${name} is full (max ${maxPins}). Unpin a chat there first.`,
    tourOffer: "All set. Take a 1-minute tour of where everything is?", takeTour: "Take the tour",
  },
  es: {
    never: "Nunca", comingSoon: "Próximamente.", expand: "Expandir barra lateral", collapse: "Contraer barra lateral",
    boards: "Tableros", tickets: "Tickets", calendar: "Calendario", connectors: "Conectores", chats: "Chats",
    skills: "Habilidades", scheduled: "Tareas programadas", yourBoards: "Tus tableros", settings: "Ajustes",
    monitors: "Monitores", monitorChanges: (n: number) => `, ${n} cambio${n === 1 ? "" : "s"} sin leer`,
    pinned: (n: number) => `Chats fijados (${n})`, approval: ", requiere aprobación", reply: ", nueva respuesta",
    pinnedTip: (n: number, shortcut: string) => `Chats fijados (${n}) ${shortcut}`,
    groupFull: (name: string) => `${name} está lleno (máx. ${maxPins}). Quita un chat de ahí primero.`,
    tourOffer: "Todo listo. ¿Quieres un recorrido de 1 minuto para ver dónde está cada cosa?", takeTour: "Hacer el recorrido",
  },
};

export function navigate(path: string) {
  history.pushState({}, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

// Notification links are in-app paths or https URLs; the latter open in the browser.
export function openLink(link: string) {
  if (!link) return;
  if (!/^https:\/\//i.test(link)) return navigate(link);
  if (window.runtime) window.runtime.BrowserOpenURL(link);
  else window.open(link, "_blank", "noopener");
}

export function ErrorText({ error }: { error: unknown }) {
  if (!error) return null;
  return (
    <p className="error" role="alert">
      {error instanceof Error ? error.message : String(error)}
    </p>
  );
}

export function formatDate(value: string | null) {
  return value ? new Date(value).toLocaleString(locale()) : text[lang()].never;
}

function currentPath() {
  if (location.pathname === "/mcp") {
    history.replaceState({}, "", "/settings");
    return "/settings";
  }
  return location.pathname;
}

function ComingSoon({ title }: { title: string }) {
  const t = useText(text);
  return (
    <div className="stack">
      <div className="page-heading">
        <div><h1>{title}</h1></div>
      </div>
      <p>{t.comingSoon}</p>
    </div>
  );
}

export default function App() {
  const t = useText(text);
  const [path, setPath] = useState(currentPath);
  const [config, setConfig] = useState<Config>();
  const [refresh, setRefresh] = useState(0);
  const [boards, setBoards] = useState<Board[]>([]);
  const [onboarding, setOnboarding] = useState(false);
  const [touring, setTouring] = useState(false);
  const [monitorChanges, setMonitorChanges] = useState(0);
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem("sidebar") === "closed",
  );
  const pins = usePins();
  const groups = usePinGroups();
  const allPinned = groups.groups.flatMap((group) => group.ids);
  const [pinnedDialog, setPinnedDialog] = useState<Connector[]>();
  const [notice, setNotice] = useState<string>();
  const [shortcutsOpen, setShortcutsOpen] = useState(false);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isShortcutsKey(event)) return;
      event.preventDefault();
      setShortcutsOpen((open) => !open);
    };
    const onRequest = () => setShortcutsOpen(true);
    addEventListener("keydown", onKeyDown);
    addEventListener("open-shortcuts", onRequest);
    return () => {
      removeEventListener("keydown", onKeyDown);
      removeEventListener("open-shortcuts", onRequest);
    };
  }, []);

  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice(undefined), 4000);
    return () => clearTimeout(timer);
  }, [notice]);

  // New chat pinned into the active group: the usual dialog, or straight in with a single connector like Cmd+N.
  const startingPinned = useRef(false);
  async function newPinnedChat() {
    const group = groups.groups.find((item) => item.id === groups.active);
    if (group && group.ids.length >= maxPins) return setNotice(t.groupFull(group.name));
    if (startingPinned.current || pinnedDialog) return;
    startingPinned.current = true;
    try {
      const connectors = await api.get<Connector[]>("/api/connectors");
      if (!connectors.length) return navigate("/ai/connectors");
      if (connectors.length > 1) return setPinnedDialog(connectors);
      openPinned(await createChatWithDefaults(connectors[0]));
    } catch (reason) {
      setNotice(reason instanceof Error ? reason.message : String(reason));
    } finally {
      startingPinned.current = false;
    }
  }

  function openPinned(chat: Chat) {
    setPinnedDialog(undefined);
    if (!pin(chat.id) || location.pathname.startsWith("/ai/chats")) return navigate(`/ai/chats/${chat.id}`);
    dispatchEvent(new CustomEvent("open-chat-link", { detail: chat.id }));
  }

  useEffect(() => {
    if (config?.chat_window) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isNewPinnedKey(event) || document.querySelector("dialog[open]")) return;
      event.preventDefault();
      newPinnedChat();
    };
    const onRequest = () => newPinnedChat();
    addEventListener("keydown", onKeyDown);
    addEventListener("new-pinned-chat", onRequest);
    return () => {
      removeEventListener("keydown", onKeyDown);
      removeEventListener("new-pinned-chat", onRequest);
    };
  }, [config, groups]);
  const pinned = usePinnedChats(pins.ids, refresh);
  const [dockOpen, setDockOpen] = useState(() => localStorage.getItem("chat-dock") === "open");
  const [dockFocus, setDockFocus] = useState<{ id: string }>();
  // The nav scrolls, which would clip a CSS [data-tip] tooltip, so it is positioned fixed instead.
  const [tip, setTip] = useState<{ text: string; top: number; left: number; focus: boolean }>();

  function showTip(button: HTMLElement, text: string, focus: boolean) {
    if (focus && !button.matches(":focus-visible")) return;
    const rect = button.getBoundingClientRect();
    setTip({ text, top: rect.top + rect.height / 2, left: rect.right + 8, focus });
  }

  const nav = useRef<HTMLElement>(null);
  const boardDrag = useRef<{ id: string; before: Board[]; dropped?: boolean }>(undefined);
  const [draggingBoard, setDraggingBoard] = useState<string>();
  const flip = useRef<Map<string, number>>(undefined);

  // FLIP: slide each board from its old row to its new one after a reorder.
  useLayoutEffect(() => {
    const before = flip.current;
    flip.current = undefined;
    if (!before || matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    nav.current?.querySelectorAll<HTMLElement>("[data-board]").forEach((item) => {
      const old = before.get(item.dataset.board!);
      const top = item.getBoundingClientRect().top;
      if (old === undefined || old === top) return;
      item.animate([{ transform: `translateY(${old - top}px)` }, { transform: "none" }], {
        duration: 200,
        easing: "ease-out",
      });
    });
  }, [boards]);

  function reorderBoards(next: Board[]) {
    flip.current = new Map(
      [...(nav.current?.querySelectorAll<HTMLElement>("[data-board]") ?? [])].map((item) => [
        item.dataset.board!,
        item.getBoundingClientRect().top,
      ]),
    );
    setBoards(next);
  }

  function boardDragProps(board: Board): ComponentProps<"button"> & Record<`data-${string}`, string | undefined> {
    return {
      draggable: true,
      "data-board": board.id,
      "data-dragging": draggingBoard === board.id ? "" : undefined,
      onDragStart: (event) => {
        event.dataTransfer.effectAllowed = "move";
        event.dataTransfer.setData("board", board.id);
        boardDrag.current = { id: board.id, before: boards };
        setDraggingBoard(board.id);
        setTip(undefined);
      },
      onDragOver: (event) => {
        const drag = boardDrag.current;
        // Skip rows still sliding, or the moved row can bounce back under the cursor.
        if (!drag || drag.id === board.id || event.currentTarget.getAnimations().length) return;
        const next = boards.filter((item) => item.id !== drag.id);
        next.splice(boards.findIndex((item) => item.id === board.id), 0, boards.find((item) => item.id === drag.id)!);
        reorderBoards(next);
      },
      onDragEnd: () => {
        const drag = boardDrag.current;
        boardDrag.current = undefined;
        setDraggingBoard(undefined);
        if (!drag) return;
        if (!drag.dropped) return reorderBoards(drag.before);
        const ids = boards.map((item) => item.id);
        if (ids.join() === drag.before.map((item) => item.id).join()) return;
        api.put("/api/board-order", { board_ids: ids }).catch(() => reorderBoards(drag.before));
      },
    };
  }

  const pinCount = useRef({ group: groups.active, count: pins.ids.length });
  const returnFocus = useRef<HTMLElement | null>(null);

  function toggleDock(open: boolean) {
    localStorage.setItem("chat-dock", open ? "open" : "closed");
    setDockOpen(open);
    if (!open) setDockFocus(undefined);
  }

  // Open the dock when a chat gets pinned, not when switching to a bigger group.
  useEffect(() => {
    const before = pinCount.current;
    if (before.group === groups.active && pins.ids.length > before.count) toggleDock(true);
    pinCount.current = { group: groups.active, count: pins.ids.length };
  }, [groups.active, pins.ids.length]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const step = groupStep(event);
      if (!step || config?.chat_window || groups.groups.length < 2) return;
      event.preventDefault();
      cycleGroup(step);
      if (location.pathname.startsWith("/ai/chats")) navigate("/ai/chats");
    };
    addEventListener("keydown", onKeyDown);
    return () => removeEventListener("keydown", onKeyDown);
  }, [config, groups.groups.length]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const composer = document.querySelector<HTMLTextAreaElement>(".chat-dock textarea");
      if (event.key === "Escape" && composer && event.target === composer) {
        composer.blur();
        returnFocus.current?.focus();
        return;
      }
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "j") return;
      if (!allPinned.length || config?.chat_window || location.pathname.startsWith("/ai/chats")) return;
      event.preventDefault();
      if (!dockOpen) return toggleDock(true);
      if (document.activeElement?.closest(".chat-dock")) return toggleDock(false);
      returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      composer?.focus();
    };
    addEventListener("keydown", onKeyDown);
    return () => removeEventListener("keydown", onKeyDown);
  }, [allPinned.length, dockOpen, config]);

  useEffect(() => {
    const onChatLink = (event: Event) => {
      const id = (event as CustomEvent<string>).detail;
      // Read the store, not render state: a chat pinned just before this event isn't rendered yet.
      const group = groupOf(id);
      if (!group || config?.chat_window || location.pathname.startsWith("/ai/chats")) return navigate(`/ai/chats/${id}`);
      setActiveGroup(group.id);
      toggleDock(true);
      setDockFocus({ id });
    };
    addEventListener("open-chat-link", onChatLink);
    return () => removeEventListener("open-chat-link", onChatLink);
  }, [pins.ids, config]);

  useEffect(() => {
    api
      .get<Board[]>("/api/boards")
      .then(setBoards)
      .catch(() => undefined);
  }, [path, refresh]);

  useEffect(() => {
    api
      .get<{ notifications: { kind: string }[] }>("/api/notifications?unread=1&limit=200")
      .then((page) => setMonitorChanges(page.notifications.filter((item) => item.kind === "monitor").length))
      .catch(() => undefined);
  }, [path, refresh]);

  useEffect(() => {
    const onPopState = () => {
      setPath(currentPath());
      setDockFocus(undefined);
    };
    addEventListener("popstate", onPopState);
    return () => removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    const unsubscribe = window.runtime?.EventsOn("db-changed", () =>
      setRefresh((current) => current + 1),
    );
    return () => unsubscribe?.();
  }, []);

  useEffect(() => window.runtime?.EventsOn("navigate", (to) => openLink(String(to))), []);

  useEffect(() => {
    api
      .get<{ show: boolean }>("/api/onboarding")
      .then((state) => setOnboarding(state.show))
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    const onStart = () => setTouring(true);
    addEventListener("start-tour", onStart);
    return () => removeEventListener("start-tour", onStart);
  }, []);

  useEffect(() => {
    const blockFileDrop = (event: DragEvent) => {
      if (event.dataTransfer?.types.includes("Files")) event.preventDefault();
    };
    addEventListener("dragover", blockFileDrop);
    addEventListener("drop", blockFileDrop);
    return () => {
      removeEventListener("dragover", blockFileDrop);
      removeEventListener("drop", blockFileDrop);
    };
  }, []);

  // A press on the <dialog> itself, not its panel, is on the backdrop. Pointerdown so drags out of the panel don't close it.
  useEffect(() => {
    const closeOnBackdrop = (event: PointerEvent) => {
      if (event.target instanceof HTMLDialogElement && event.target.open) event.target.close();
    };
    addEventListener("pointerdown", closeOnBackdrop);
    return () => removeEventListener("pointerdown", closeOnBackdrop);
  }, []);

  useEffect(() => {
    api
      .get<Config>("/api/config")
      .then((loaded) => {
        if (loaded.open_path) navigate(loaded.open_path);
        setConfig(loaded);
      })
      .catch(() => setConfig({ show_hidden: false, version: "", chat_window: "", open_path: "" }));
  }, []);

  // Pins live only in the UI, so the cleanup request carries every group's; the server skips it when cleanup is off.
  const pinIds = useRef(allPinned);
  pinIds.current = allPinned;
  useEffect(() => {
    if (!config || config.chat_window) return;
    const run = () => api.post("/api/chats/cleanup", { keep: pinIds.current }).catch(() => undefined);
    run();
    const timer = setInterval(run, 24 * 60 * 60 * 1000);
    return () => clearInterval(timer);
  }, [config]);

  // The side panel is hidden on chat routes; remember the pinned chat viewed there for when it comes back.
  useEffect(() => {
    const id = path.match(/^\/ai\/chats\/([^/]+)$/)?.[1];
    if (id && pins.ids.includes(id)) rememberPinnedChat(id);
  }, [path, pins.ids]);

  if (!config) return null;
  if (config.chat_window)
    return (
      <main className="chat-window">
        <Chats id={config.chat_window} refresh={refresh} windowed />
      </main>
    );

  const boardMatch = path.match(/^\/boards\/([^/]+)$/);
  const newTicketMatch = path.match(/^\/boards\/([^/]+)\/new$/);
  const printMatch = path.match(/^\/print\/tickets\/([^/]+)$/);
  const ticketMatch = path.match(/^\/tickets\/([^/]+)$/);
  const isCalendar = path === "/calendar";
  const isTickets = path === "/tickets";
  const aiPages: Record<string, string> = {
  };
  let page: ReactNode = <BoardList refresh={refresh} />;
  if (boardMatch)
    page = (
      <BoardView id={decodeURIComponent(boardMatch[1])} refresh={refresh} />
    );
  if (newTicketMatch) page = <NewTicketPage key={newTicketMatch[1]} boardId={decodeURIComponent(newTicketMatch[1])} />;
  if (printMatch)
    page = (
      <PrintTicket id={decodeURIComponent(printMatch[1])} refresh={refresh} />
    );
  if (ticketMatch) {
    page = (
      <TicketDetail
        id={decodeURIComponent(ticketMatch[1])}
        config={config}
        refresh={refresh}
      />
    );
  }
  if (isCalendar) page = <Calendar refresh={refresh} />;
  const monitorMatch = path.match(/^\/monitors(?:\/([^/]+))?$/);
  if (monitorMatch) page = <Monitors id={monitorMatch[1] && decodeURIComponent(monitorMatch[1])} refresh={refresh} />;
  if (isTickets) page = <Tickets refresh={refresh} />;
  if (path === "/ai/connectors") page = <Connectors refresh={refresh} />;
  const chatMatch = path.match(/^\/ai\/chats(?:\/([^/]+))?$/);
  if (chatMatch)
    page = (
      <Chats id={chatMatch[1] && decodeURIComponent(chatMatch[1])} refresh={refresh} pinned={pinned} />
    );
  const skillMatch = path.match(/^\/ai\/skills(?:\/([^/]+))?$/);
  if (skillMatch)
    page = <Skills id={skillMatch[1] && decodeURIComponent(skillMatch[1])} refresh={refresh} />;
  if (path === "/ai/mcp") page = <McpServers refresh={refresh} />;
  const scheduledMatch = path.match(/^\/ai\/scheduled(?:\/([^/]+))?$/);
  if (scheduledMatch)
    page = <ScheduledTasks id={scheduledMatch[1] && decodeURIComponent(scheduledMatch[1])} refresh={refresh} />;
  if (aiPages[path]) page = <ComingSoon title={aiPages[path]} />;
  if (path === "/settings") page = <Settings />;
  if (path === "/notifications") page = <NotificationsPage refresh={refresh} />;

  const showDock = allPinned.length > 0 && !chatMatch;
  const onScreen = (link: string) => {
    if (link === path) return true;
    const id = link.match(/^\/ai\/chats\/([^/]+)$/)?.[1];
    if (!id || !pins.ids.includes(id)) return false;
    // Pinned chats are all on screen in the Chats grid (shown for a pinned or no chat); a split shows its panes, one-tab view and the dock only one.
    const group = groups.groups.find((group) => group.id === groups.active);
    if (chatMatch && group?.view === "one") return (chatMatch[1] ?? shownPin(pins.ids)) === id;
    if (chatMatch && group?.view === "split")
      return (!chatMatch[1] || pins.ids.includes(chatMatch[1])) && (chatMatch[1] === id || splitLeaves(pruneSplit(group.split, pins.ids) ?? { chat: shownPin(pins.ids) }).includes(id));
    return chatMatch ? !chatMatch[1] || pins.ids.includes(chatMatch[1]) : dockOpen && lastPinnedChat() === id;
  };
  const states = pins.ids.map((id) => pinned.status[id]);
  const dockAlert = dockOpen ? undefined : states.includes("approval") ? "approval" : states.includes("reply") ? "reply" : undefined;

  const navItem = (
    to: string,
    label: string,
    mark: ReactNode,
    active: boolean,
    props?: ComponentProps<"button"> & Record<`data-${string}`, string | undefined>,
  ) => (
    <button
      {...props}
      key={to}
      className="nav-item"
      aria-label={props?.["aria-label"] ?? label}
      aria-current={active ? "page" : undefined}
      onMouseEnter={(event) => showTip(event.currentTarget, label, false)}
      onFocus={(event) => showTip(event.currentTarget, label, true)}
      onMouseLeave={() => setTip(undefined)}
      onBlur={() => setTip(undefined)}
      onClick={() => navigate(to)}
    >
      <span className="nav-mark">{mark}</span>
      <span className="nav-label">{label}</span>
    </button>
  );

  return (
    <div className="app">
      <aside className={`sidebar ${collapsed ? "collapsed" : ""}`}>
        <div className="sidebar-head">
          <span className="brand nav-label">Patio</span>
          <IconButton
            className="ghost"
            icon={collapsed ? "chevronsRight" : "chevronsLeft"}
              label={collapsed ? t.expand : t.collapse}
            aria-expanded={!collapsed}
            onClick={() => {
              localStorage.setItem("sidebar", collapsed ? "open" : "closed");
              setCollapsed(!collapsed);
            }}
          />
        </div>
        <nav
          ref={nav}
          onScroll={() => setTip(undefined)}
          onDragOver={(event) => boardDrag.current && event.preventDefault()}
          onDrop={(event) => {
            if (!boardDrag.current) return;
            event.preventDefault();
            boardDrag.current.dropped = true;
          }}
        >
          {navItem("/", t.boards, <Icon name="boards" />, path === "/", { "data-tour": "boards" })}
          {navItem("/tickets", t.tickets, <Icon name="ticket" />, isTickets, { "data-tour": "tickets" })}
          {navItem("/calendar", t.calendar, <Icon name="calendar" />, isCalendar, { "data-tour": "calendar" })}
          {navItem(
            "/monitors",
            t.monitors,
            <>
              <Icon name="eye" />
              {monitorChanges > 0 && (
                <span className="nav-count" aria-hidden="true">
                  {monitorChanges > 9 ? "9+" : monitorChanges}
                </span>
              )}
            </>,
            !!monitorMatch,
            { "aria-label": t.monitors + (monitorChanges ? t.monitorChanges(monitorChanges) : ""), "data-tour": "monitors" },
          )}
          <div className="ai-nav-group">
            <p className="sidebar-label nav-label">AI</p>
            {navItem(
              "/ai/connectors",
              t.connectors,
              <Icon name="connector" />,
              path === "/ai/connectors",
              { "data-tour": "connectors" },
            )}
            {navItem("/ai/chats", t.chats, <Icon name="chat" />, !!chatMatch, { "data-tour": "chats" })}
            {navItem("/ai/skills", t.skills, <Icon name="sparkles" />, !!skillMatch, { "data-tour": "skills" })}
            {navItem("/ai/mcp", "MCP", <Icon name="plug" />, path === "/ai/mcp", { "data-tour": "mcp" })}
          </div>
          {navItem("/ai/scheduled", t.scheduled, <Icon name="clock" />, !!scheduledMatch, { "data-tour": "scheduled" })}
          {boards.length > 0 && <p className="sidebar-label nav-label">{t.yourBoards}</p>}
          {boards.map((board) =>
            navItem(
              `/boards/${board.id}`,
              board.name,
              board.short_name.slice(0, 2),
              (boardMatch ?? newTicketMatch)?.[1] === board.id,
              boardDragProps(board),
            ),
          )}
        </nav>
        <div className="sidebar-foot" data-tour="foot">
          {navItem("/settings", t.settings, <Icon name="gear" />, path === "/settings")}
          <ThemeToggle />
          <Notifications refresh={refresh} onScreen={onScreen} />
          {showDock && (
            <button
              className="icon ghost dock-toggle"
              aria-label={`${t.pinned(pins.ids.length)}${dockAlert === "approval" ? t.approval : dockAlert === "reply" ? t.reply : ""}`}
              data-tip={t.pinnedTip(pins.ids.length, dockShortcut)}
              aria-expanded={dockOpen}
              onClick={() => toggleDock(!dockOpen)}
            >
              <Icon name="panelRight" />
              {dockAlert && (
                <span className="dock-count" aria-hidden="true">
                  <Icon name={statusIcons[dockAlert]} />
                </span>
              )}
            </button>
          )}
        </div>
        {tip && (
          <div className={tip.focus ? "rail-tip wide focus" : "rail-tip wide"} aria-hidden="true" style={{ top: tip.top, left: tip.left }}>
            {tip.text}
          </div>
        )}
      </aside>
      <main>{page}</main>
      {showDock && dockOpen && (
        <ChatDock
          ids={pins.ids}
          chats={pinned.chats}
          status={pinned.status}
          refresh={refresh}
          focus={dockFocus}
          page={
            ticketMatch
              ? { kind: "ticket", id: decodeURIComponent(ticketMatch[1]) }
              : boardMatch
                ? { kind: "board", id: decodeURIComponent(boardMatch[1]) }
                : undefined
          }
          onSeen={pinned.seen}
          onClose={() => toggleDock(false)}
        />
      )}
      {pinnedDialog && <NewChatDialog connectors={pinnedDialog} onClose={() => setPinnedDialog(undefined)} onCreated={openPinned} />}
      {shortcutsOpen && <ShortcutsDialog onClose={() => setShortcutsOpen(false)} />}
      {onboarding && (
        <Onboarding
          onDone={async () => {
            setOnboarding(false);
            setTouring(await confirmDialog(t.tourOffer, t.takeTour, false));
          }}
        />
      )}
      {touring && <Tour onClose={() => setTouring(false)} />}
      {notice && (
        <p className="app-notice" role="status">
          {notice}
        </p>
      )}
      {config.version && <span className="app-version">{config.version}</span>}
    </div>
  );
}
