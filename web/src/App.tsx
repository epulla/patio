import { useEffect, useState, type ReactNode } from "react";
import BoardList from "./BoardList";
import BoardView from "./BoardView";
import TicketDetail from "./TicketDetail";
import PrintTicket from "./PrintTicket";
import Calendar from "./Calendar";
import Mcp from "./Mcp";
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

export default function App() {
  const [path, setPath] = useState(location.pathname);
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
    const onPopState = () => setPath(location.pathname);
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
  const isMcp = path === "/mcp";
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
  if (isMcp) page = <Mcp />;

  const navItem = (to: string, label: string, mark: ReactNode, active: boolean) => (
    <button
      key={to}
      className="nav-item"
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
          {navItem("/calendar", "Calendar", <Icon name="calendar" />, isCalendar)}
          {navItem("/mcp", "MCP", <Icon name="plug" />, isMcp)}
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
        </div>
      </aside>
      <main>{page}</main>
      {config.version && <span className="app-version">{config.version}</span>}
    </div>
  );
}
