import { useEffect, useState, type ReactNode } from "react";
import BoardList from "./BoardList";
import BoardView from "./BoardView";
import TicketDetail from "./TicketDetail";
import PrintTicket from "./PrintTicket";
import Calendar from "./Calendar";
import Settings from "./Settings";
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
  const [config, setConfig] = useState<Config>({ show_hidden: false, version: "" });
  const [refresh, setRefresh] = useState(0);
  const [boards, setBoards] = useState<Board[]>([]);
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem("sidebar") === "closed",
  );

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

  useEffect(() => {
    api
      .get<Config>("/api/config")
      .then(setConfig)
      .catch(() => undefined);
  }, []);

  const boardMatch = path.match(/^\/boards\/([^/]+)$/);
  const printMatch = path.match(/^\/print\/tickets\/([^/]+)$/);
  const ticketMatch = path.match(/^\/tickets\/([^/]+)$/);
  const isCalendar = path === "/calendar";
  const isTickets = path === "/tickets";
  const aiPages: Record<string, string> = {
    "/ai/connectors": "Connectors",
    "/ai/chats": "Chats",
    "/ai/skills": "Skills",
    "/ai/mcp": "MCP",
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
  if (isTickets) page = <ComingSoon title="Tickets" />;
  if (aiPages[path]) page = <ComingSoon title={aiPages[path]} />;
  if (path === "/settings") page = <Settings />;

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
          <span className="brand nav-label">my-tickets</span>
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
            {navItem("/ai/chats", "Chats", <Icon name="chat" />, path === "/ai/chats")}
            {navItem("/ai/skills", "Skills", <Icon name="sparkles" />, path === "/ai/skills")}
            {navItem("/ai/mcp", "MCP", <Icon name="plug" />, path === "/ai/mcp")}
          </div>
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
          <ThemeToggle />
          <IconButton
            className="ghost"
            icon="gear"
            label="Settings"
            aria-current={path === "/settings" ? "page" : undefined}
            onClick={() => navigate("/settings")}
          />
        </div>
      </aside>
      <main>{page}</main>
      {config.version && <span className="app-version">{config.version}</span>}
    </div>
  );
}
