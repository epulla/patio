import { useEffect, useLayoutEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import BoardList from "./BoardList";
import BoardView, { NewTicketPage } from "./BoardView";
import TicketDetail from "./TicketDetail";
import PrintTicket from "./PrintTicket";
import Calendar from "./Calendar";
import Connectors from "./Connectors";
import Chats from "./Chats";
import ChatDock, { dockShortcut, usePinnedChats } from "./ChatDock";
import { statusIcons } from "./chatStatus";
import { getPins, rememberPinnedChat, usePins } from "./pins";
import McpServers from "./McpServers";
import Skills from "./Skills";
import ScheduledTasks from "./ScheduledTasks";
import Settings from "./Settings";
import Tickets from "./Tickets";
import Notifications, { NotificationsPage } from "./Notifications";
import { api, type Board, type Config } from "./api";
export type { Config } from "./api";
import { ThemeToggle } from "./theme";
import { Icon, IconButton } from "./icons";

export function navigate(path: string) {
  history.pushState({}, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
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
  return value ? new Date(value).toLocaleString() : "Never";
}

function currentPath() {
  if (location.pathname === "/mcp") {
    history.replaceState({}, "", "/settings");
    return "/settings";
  }
  return location.pathname;
}

function ComingSoon({ title }: { title: string }) {
  return (
    <div className="stack">
      <div className="page-heading">
        <div><h1>{title}</h1></div>
      </div>
      <p>Coming soon.</p>
    </div>
  );
}

export default function App() {
  const [path, setPath] = useState(currentPath);
  const [config, setConfig] = useState<Config>();
  const [refresh, setRefresh] = useState(0);
  const [boards, setBoards] = useState<Board[]>([]);
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem("sidebar") === "closed",
  );
  const pins = usePins();
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

  const pinCount = useRef(pins.ids.length);
  const returnFocus = useRef<HTMLElement | null>(null);

  function toggleDock(open: boolean) {
    localStorage.setItem("chat-dock", open ? "open" : "closed");
    setDockOpen(open);
    if (!open) setDockFocus(undefined);
  }

  useEffect(() => {
    if (pins.ids.length > pinCount.current) toggleDock(true);
    pinCount.current = pins.ids.length;
  }, [pins.ids.length]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const composer = document.querySelector<HTMLTextAreaElement>(".chat-dock textarea");
      if (event.key === "Escape" && composer && event.target === composer) {
        composer.blur();
        returnFocus.current?.focus();
        return;
      }
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "j") return;
      if (!pins.ids.length || config?.chat_window || location.pathname.startsWith("/ai/chats")) return;
      event.preventDefault();
      if (!dockOpen) return toggleDock(true);
      if (document.activeElement?.closest(".chat-dock")) return toggleDock(false);
      returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      composer?.focus();
    };
    addEventListener("keydown", onKeyDown);
    return () => removeEventListener("keydown", onKeyDown);
  }, [pins.ids.length, dockOpen, config]);

  useEffect(() => {
    const onChatLink = (event: Event) => {
      const id = (event as CustomEvent<string>).detail;
      // Read the store, not render state: a chat pinned just before this event isn't rendered yet.
      if (!getPins().ids.includes(id) || config?.chat_window || location.pathname.startsWith("/ai/chats"))
        return navigate(`/ai/chats/${id}`);
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

  useEffect(() => window.runtime?.EventsOn("navigate", (to) => navigate(String(to))), []);

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

  // Pins live only in the UI, so the cleanup request carries them; the server skips it when cleanup is off.
  const pinIds = useRef(pins.ids);
  pinIds.current = pins.ids;
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
  if (isTickets) page = <Tickets refresh={refresh} />;
  if (path === "/ai/connectors") page = <Connectors refresh={refresh} />;
  const chatMatch = path.match(/^\/ai\/chats(?:\/([^/]+))?$/);
  if (chatMatch)
    page = (
      <Chats id={chatMatch[1] && decodeURIComponent(chatMatch[1])} refresh={refresh} />
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

  const showDock = pins.ids.length > 0 && !chatMatch;
  const onScreen = (link: string) => {
    if (link === path) return true;
    const id = link.match(/^\/ai\/chats\/([^/]+)$/)?.[1];
    if (!id || !pins.ids.includes(id)) return false;
    // Pinned chats are on screen in the Chats grid (shown for a pinned or no chat) or in the open dock.
    return chatMatch ? !chatMatch[1] || pins.ids.includes(chatMatch[1]) : dockOpen;
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
      aria-label={label}
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
            label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
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
          {navItem("/", "Boards", <Icon name="boards" />, path === "/")}
          {navItem("/tickets", "Tickets", <Icon name="ticket" />, isTickets)}
          {navItem("/calendar", "Calendar", <Icon name="calendar" />, isCalendar)}
          <div className="ai-nav-group">
            <p className="sidebar-label nav-label">AI</p>
            {navItem(
              "/ai/connectors",
              "Connectors",
              <Icon name="connector" />,
              path === "/ai/connectors",
            )}
            {navItem("/ai/chats", "Chats", <Icon name="chat" />, !!chatMatch)}
            {navItem("/ai/skills", "Skills", <Icon name="sparkles" />, !!skillMatch)}
            {navItem("/ai/mcp", "MCP", <Icon name="plug" />, path === "/ai/mcp")}
          </div>
          {navItem("/ai/scheduled", "Scheduled tasks", <Icon name="clock" />, !!scheduledMatch)}
          {boards.length > 0 && <p className="sidebar-label nav-label">Your boards</p>}
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
        <div className="sidebar-foot">
          {navItem("/settings", "Settings", <Icon name="gear" />, path === "/settings")}
          <ThemeToggle />
          <Notifications refresh={refresh} onScreen={onScreen} />
          {showDock && (
            <button
              className="icon ghost dock-toggle"
              aria-label={`Pinned chats (${pins.ids.length})${dockAlert === "approval" ? ", approval needed" : dockAlert === "reply" ? ", new reply" : ""}`}
              data-tip={`Pinned chats (${pins.ids.length}) ${dockShortcut}`}
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
      {config.version && <span className="app-version">{config.version}</span>}
    </div>
  );
}
