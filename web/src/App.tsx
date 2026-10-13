import { useEffect, useRef, useState, type ReactNode } from "react";
import BoardList from "./BoardList";
import BoardView from "./BoardView";
import TicketDetail from "./TicketDetail";
import PrintTicket from "./PrintTicket";
import Calendar from "./Calendar";
import Connectors from "./Connectors";
import Chats from "./Chats";
import ChatDock, { statusMarks, usePinnedChats } from "./ChatDock";
import { usePins } from "./pins";
import McpServers from "./McpServers";
import Skills from "./Skills";
import ScheduledTasks from "./ScheduledTasks";
import Settings from "./Settings";
import Tickets from "./Tickets";
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

  const pinCount = useRef(pins.ids.length);
  const returnFocus = useRef<HTMLElement | null>(null);

  function toggleDock(open: boolean) {
    localStorage.setItem("chat-dock", open ? "open" : "closed");
    setDockOpen(open);
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
    api
      .get<Board[]>("/api/boards")
      .then(setBoards)
      .catch(() => undefined);
  }, [path, refresh]);

  useEffect(() => {
    const onPopState = () => setPath(currentPath());
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

  useEffect(() => {
    api
      .get<Config>("/api/config")
      .then((loaded) => {
        if (loaded.open_path) navigate(loaded.open_path);
        setConfig(loaded);
      })
      .catch(() => setConfig({ show_hidden: false, version: "", chat_window: "", open_path: "" }));
  }, []);

  if (!config) return null;
  if (config.chat_window)
    return (
      <main className="chat-window">
        <Chats id={config.chat_window} refresh={refresh} windowed />
      </main>
    );

  const boardMatch = path.match(/^\/boards\/([^/]+)$/);
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

  const showDock = pins.ids.length > 0 && !chatMatch;
  const states = Object.values(pinned.status);
  const dockAlert = dockOpen ? undefined : states.includes("approval") ? "approval" : states.includes("reply") ? "reply" : undefined;

  const navItem = (to: string, label: string, mark: ReactNode, active: boolean) => (
    <button
      key={to}
      className="nav-item"
      aria-label={label}
      aria-current={active ? "page" : undefined}
      title={label}
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
        <nav>
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
              boardMatch?.[1] === board.id,
            ),
          )}
        </nav>
        <div className="sidebar-foot">
          {navItem("/settings", "Settings", <Icon name="gear" />, path === "/settings")}
          <ThemeToggle />
          {showDock && (
            <button
              className="icon ghost dock-toggle"
              aria-label={`Pinned chats (${pins.ids.length})${dockAlert === "approval" ? ", approval needed" : dockAlert === "reply" ? ", new reply" : ""}`}
              data-tip={`Pinned chats (${pins.ids.length}) ⌘J`}
              aria-expanded={dockOpen}
              onClick={() => toggleDock(!dockOpen)}
            >
              <Icon name="chat" />
              <span className="dock-count" aria-hidden="true">{dockAlert ? statusMarks[dockAlert] : pins.ids.length}</span>
            </button>
          )}
        </div>
      </aside>
      <main>{page}</main>
      {showDock && dockOpen && (
        <ChatDock
          ids={pins.ids}
          chats={pinned.chats}
          status={pinned.status}
          refresh={refresh}
          onSeen={pinned.seen}
          onClose={() => toggleDock(false)}
        />
      )}
      {config.version && <span className="app-version">{config.version}</span>}
    </div>
  );
}
