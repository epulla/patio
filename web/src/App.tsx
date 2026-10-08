import { useEffect, useState, type ReactNode } from "react";
import BoardList from "./BoardList";
import BoardView from "./BoardView";
import TicketDetail from "./TicketDetail";
import PrintTicket from "./PrintTicket";
import Calendar from "./Calendar";
import { api } from "./api";
import { ThemeToggle } from "./theme";

export type Config = { show_hidden: boolean };

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
  const [config, setConfig] = useState<Config>({ show_hidden: false });
  const [refresh, setRefresh] = useState(0);

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

  return (
    <>
      <header className="top">
        <button className="brand" onClick={() => navigate("/")}>
          my-tickets
        </button>
        <span>Local workspace</span>
        <button className="back" onClick={() => navigate("/calendar")}>
          Calendar
        </button>
        <ThemeToggle />
      </header>
      <main>{page}</main>
    </>
  );
}
