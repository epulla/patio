import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import Menu from "./Menu";
import Markdown from "./Markdown";
import { Icon, IconButton, type IconName } from "./icons";
import { api } from "./api";
import { ErrorText, navigate } from "./App";
import { formatAgo } from "./Connectors";

type Notification = {
  id: string;
  kind: string;
  title: string;
  body: string;
  link: string;
  created_at: string;
  read_at: string | null;
};
type Page = { notifications: Notification[]; unread: number; next_cursor: string | null };

const kindIcons: Record<string, IconName> = { chat: "chat", scheduled: "clock", calendar: "calendar", change: "sparkles" };

function markRead(item: Notification) {
  return { ...item, read_at: item.read_at ?? new Date().toISOString() };
}

function RowBody({ item, body, onOpen }: { item: Notification; body: ReactNode; onOpen?: () => void }) {
  const title = (
    <>
      {!item.read_at && <span className="sr-only">Unread: </span>}
      {item.title}
    </>
  );
  return (
    <>
      <span className="notif-icon" aria-hidden="true">
        <Icon name={kindIcons[item.kind] ?? "dot"} />
      </span>
      <span className="notif-text">
        {onOpen ? (
          <button type="button" className="notif-title" onClick={onOpen}>
            {title}
          </button>
        ) : (
          <span className="notif-title">{title}</span>
        )}
        {body}
      </span>
      <time className="notif-time" dateTime={item.created_at}>
        {formatAgo(item.created_at)}
      </time>
      {!item.read_at && <span className="notif-dot" aria-hidden="true" />}
    </>
  );
}

export default function Notifications({ refresh, onScreen }: { refresh: number; onScreen: (link: string) => boolean }) {
  const [list, setList] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const [toasts, setToasts] = useState<Notification[]>([]);
  const seen = useRef<Set<string>>(undefined);

  useEffect(() => {
    api
      .get<Page>("/api/notifications?limit=10")
      .then((value) => {
        setList(value.notifications);
        setUnread(value.unread);
        // Only notifications that arrive after the first load pop up as toasts.
        const first = !seen.current;
        const known = (seen.current ??= new Set());
        const fresh = value.notifications.filter((item) => !item.read_at && !known.has(item.id));
        fresh.forEach((item) => known.add(item.id));
        if (!first && document.hasFocus())
          setToasts((current) => [...current, ...fresh.filter((item) => !onScreen(item.link))]);
      })
      .catch(() => undefined);
  }, [refresh]);

  useEffect(() => {
    if (document.hasFocus()) list.filter((item) => !item.read_at && onScreen(item.link)).forEach(read);
  });

  const reading = useRef(new Set<string>());
  function read(item: Notification) {
    if (item.read_at || reading.current.has(item.id)) return;
    reading.current.add(item.id);
    setList((current) => current.map((x) => (x.id === item.id ? markRead(x) : x)));
    setUnread((count) => Math.max(0, count - 1));
    api.post(`/api/notifications/${item.id}/read`, {}).catch(() => undefined);
  }

  function readAll() {
    setList((current) => current.map(markRead));
    setUnread(0);
    api.post("/api/notifications/read-all", {}).catch(() => undefined);
  }

  function open(item: Notification) {
    read(item);
    dismiss(item.id);
    navigate(item.link);
  }

  function dismiss(id: string) {
    setToasts((current) => current.filter((x) => x.id !== id));
  }

  return (
    <div className="notifications">
      <Menu
        label={unread ? `Notifications, ${unread} unread` : "Notifications"}
        buttonClassName="icon ghost notif-toggle"
        button={
          <>
            {unread > 0 && (
              <span className="notif-count" aria-hidden="true">
                {unread > 99 ? "99+" : unread}
              </span>
            )}
            <Icon name="bell" />
          </>
        }
      >
        {(close) => (
          <>
            <div className="notif-head">
              <strong>Notifications</strong>
              {unread > 0 && <IconButton type="button" className="ghost" icon="check" label="Mark all read" onClick={readAll} />}
              <IconButton
                type="button"
                className="ghost"
                icon="list"
                label="See all notifications"
                onClick={() => (close(false), navigate("/notifications"))}
              />
            </div>
            {list.length === 0 ? (
              <p className="muted notif-empty">You're all caught up.</p>
            ) : (
              <div className="notif-list">
                {list.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    role="menuitem"
                    className="notif-row"
                    data-unread={item.read_at ? undefined : ""}
                    onClick={() => (close(false), open(item))}
                  >
                    <RowBody item={item} body={item.body && <span className="notif-body">{plain(item.body)}</span>} />
                  </button>
                ))}
              </div>
            )}
          </>
        )}
      </Menu>
      {createPortal(
        <div className="notif-toasts" role="status">
          {toasts.map((item) => (
            <Toast key={item.id} item={item} onOpen={() => open(item)} onClose={() => dismiss(item.id)} />
          ))}
        </div>,
        document.body,
      )}
    </div>
  );
}

function plain(markdown: string) {
  return markdown.replace(/\[([^\]]*)\]\([^)]*\)/g, "$1");
}

function Toast({ item, onOpen, onClose }: { item: Notification; onOpen: () => void; onClose: () => void }) {
  useEffect(() => {
    const timer = setTimeout(onClose, 6000);
    return () => clearTimeout(timer);
  }, []);
  return (
    <div className="notif-toast">
      <button type="button" className="notif-row" onClick={onOpen}>
        <RowBody item={item} body={item.body && <span className="notif-body">{plain(item.body)}</span>} />
      </button>
      <IconButton type="button" className="ghost" icon="x" label="Dismiss notification" onClick={onClose} />
    </div>
  );
}

function dayLabel(value: string) {
  const day = new Date(value).toDateString();
  const today = new Date();
  if (day === today.toDateString()) return "Today";
  if (day === new Date(today.getTime() - 86_400_000).toDateString()) return "Yesterday";
  return new Date(value).toLocaleDateString(undefined, { weekday: "long", month: "short", day: "numeric" });
}

export function NotificationsPage({ refresh }: { refresh: number }) {
  const [filter, setFilter] = useState<"all" | "unread">("all");
  const [items, setItems] = useState<Notification[]>();
  const [cursor, setCursor] = useState<string | null>(null);
  const [unread, setUnread] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<unknown>();
  const current = useRef(filter);
  const loaded = useRef<string>(undefined);
  const query = (after?: string | null) =>
    `/api/notifications?limit=50${filter === "unread" ? "&unread=1" : ""}${after ? `&cursor=${encodeURIComponent(after)}` : ""}`;

  // A filter switch replaces the list; a refresh only merges in the newest page so loaded pages stay.
  useEffect(() => {
    current.current = filter;
    api
      .get<Page>(query())
      .then((page) => {
        if (current.current !== filter) return;
        setUnread(page.unread);
        setError(undefined);
        if (loaded.current !== filter) {
          loaded.current = filter;
          setItems(page.notifications);
          setCursor(page.next_cursor);
          return;
        }
        const ids = new Set(page.notifications.map((item) => item.id));
        setItems((list) => [...page.notifications, ...(list ?? []).filter((item) => !ids.has(item.id))]);
      })
      .catch(setError);
  }, [refresh, filter]);

  function loadMore() {
    setLoadingMore(true);
    api
      .get<Page>(query(cursor))
      .then((page) => {
        if (current.current !== filter) return;
        setItems((list) => {
          const ids = new Set((list ?? []).map((item) => item.id));
          return [...(list ?? []), ...page.notifications.filter((item) => !ids.has(item.id))];
        });
        setCursor(page.next_cursor);
      })
      .catch(setError)
      .finally(() => setLoadingMore(false));
  }

  function read(item: Notification) {
    if (item.read_at) return;
    setItems((list) => list?.map((x) => (x.id === item.id ? markRead(x) : x)));
    setUnread((count) => Math.max(0, count - 1));
    api.post(`/api/notifications/${item.id}/read`, {}).catch(setError);
  }

  function readAll() {
    setItems((list) => list?.map(markRead));
    setUnread(0);
    api.post("/api/notifications/read-all", {}).catch(setError);
  }

  const groups: [string, Notification[]][] = [];
  for (const item of items ?? []) {
    const label = dayLabel(item.created_at);
    if (groups.at(-1)?.[0] === label) groups.at(-1)![1].push(item);
    else groups.push([label, [item]]);
  }

  return (
    <div className="stack notif-page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Inbox</p>
          <h1>Notifications</h1>
        </div>
        <div className="actions">
          <button type="button" onClick={readAll} disabled={!unread}>
            <Icon name="check" /> Mark all read
          </button>
        </div>
      </div>
      <p className="muted">Chat replies, approvals, reminders, scheduled runs, and changes Martin made. Kept for 7 days.</p>
      <ErrorText error={error} />
      <div className="tabs" role="tablist" aria-label="Filter notifications">
        {(
          [
            ["all", "All"],
            ["unread", unread ? `Unread (${unread})` : "Unread"],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={filter === key}
            aria-controls="notif-panel"
            onClick={() => {
              if (filter === key) return;
              loaded.current = undefined;
              setItems(undefined);
              setCursor(null);
              setFilter(key);
            }}
          >
            {label}
          </button>
        ))}
      </div>
      <div id="notif-panel" role="tabpanel" className="stack notif-groups">
        {items?.length === 0 && (
          <p className="panel muted notif-page-empty">
            {filter === "unread" ? "You're all caught up." : "No notifications in the last 7 days."}
          </p>
        )}
        {groups.map(([label, list]) => (
          <section key={label} aria-label={label}>
            <h2 className="notif-day">{label}</h2>
            <ul className="panel notif-page-list">
              {list.map((item) => (
                <li
                  key={item.id}
                  className="notif-row"
                  data-unread={item.read_at ? undefined : ""}
                  onClickCapture={() => read(item)}
                >
                  <RowBody
                    item={item}
                    onOpen={() => navigate(item.link)}
                    body={item.body && <div className="notif-body">
                      <Markdown text={item.body} />
                    </div>}
                  />
                </li>
              ))}
            </ul>
          </section>
        ))}
        {cursor && (
          <button type="button" className="notif-more" onClick={loadMore} disabled={loadingMore}>
            {loadingMore ? "Loading…" : "Load more"}
          </button>
        )}
      </div>
    </div>
  );
}
