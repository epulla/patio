import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import Menu from "./Menu";
import Markdown from "./Markdown";
import { Icon, IconButton, type IconName } from "./icons";
import { api } from "./api";
import { ErrorText, navigate, openLink } from "./App";
import { lang, locale, useText } from "./i18n";

const text = {
  en: { unread: "Unread: ", notifications: "Notifications", caughtUp: "You're all caught up.", new: "New: ", all: "See all notifications", now: "now", dismiss: "Dismiss notification", today: "Today", yesterday: "Yesterday", inbox: "Inbox", description: "Chat replies, approvals, reminders, scheduled runs, and changes Martin made. Kept for 7 days. Opening this page or the bell marks them read.", empty: "No notifications in the last 7 days.", loading: "Loading…", more: "Load more" },
  es: { unread: "Sin leer: ", notifications: "Notificaciones", caughtUp: "Estás al día.", new: "Nuevo: ", all: "Ver todas las notificaciones", now: "ahora", dismiss: "Descartar notificación", today: "Hoy", yesterday: "Ayer", inbox: "Bandeja de entrada", description: "Respuestas de chat, aprobaciones, recordatorios, tareas programadas y cambios de Martin. Se conservan durante 7 días. Al abrir esta página o la campana, se marcan como leídas.", empty: "No hay notificaciones de los últimos 7 días.", loading: "Cargando…", more: "Cargar más" },
};

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

const kindIcons: Record<string, IconName> = { chat: "chat", approval: "hand", scheduled: "clock", calendar: "calendar", change: "sparkles", health: "plug", custom: "bell", monitor: "eye" };

function markRead(item: Notification) {
  return { ...item, read_at: item.read_at ?? new Date().toISOString() };
}

function RowBody({ item, body, onOpen }: { item: Notification; body: ReactNode; onOpen?: () => void }) {
  const t = useText(text);
  const title = (
    <>
      {!item.read_at && <span className="sr-only">{t.unread}</span>}
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
        {shortAgo(item.created_at)}
      </time>
      {!item.read_at && <span className="notif-dot" aria-hidden="true" />}
    </>
  );
}

export default function Notifications({ refresh, onScreen }: { refresh: number; onScreen: (link: string) => boolean }) {
  const t = useText(text);
  const [list, setList] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const [toasts, setToasts] = useState<Notification[]>([]);
  const seen = useRef<Set<string>>(undefined);

  useEffect(() => {
    api
      .get<Page>("/api/notifications?limit=12")
      .then((value) => {
        // Only notifications that arrive after the first load pop up as toasts.
        const first = !seen.current;
        const known = (seen.current ??= new Set());
        const fresh = value.notifications.filter((item) => !item.read_at && !known.has(item.id));
        fresh.forEach((item) => known.add(item.id));
        // A reply in a chat the user is looking at needs no notification at all.
        const drop = first || !document.hasFocus() ? [] : fresh.filter((item) => item.kind === "chat" && onScreen(item.link));
        drop.forEach((item) => api.delete(`/api/notifications/${item.id}`).catch(() => undefined));
        // The server alerts natively for approvals, schedules and reminders; chat replies and custom ones only when Patio is in the background.
        if (!first && !document.hasFocus())
          fresh
            .filter((item) => item.kind === "chat" || item.kind === "custom")
            .forEach((item) => api.post(`/api/notifications/${item.id}/native`, {}).catch(() => undefined));
        setList(value.notifications.filter((item) => !drop.includes(item)));
        setUnread(value.unread - drop.length);
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
    openLink(item.link);
  }

  function dismiss(id: string) {
    setToasts((current) => current.filter((x) => x.id !== id));
  }

  const [popoverOpen, setPopoverOpen] = useState(false);
  const fresh = useMarkSeen(list, popoverOpen, unread > 0, readAll);

  return (
    <div className="notifications">
      <Menu
        label={unread ? `${t.notifications}, ${unread} ${lang() === "es" ? "sin leer" : "unread"}` : t.notifications}
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
            <Opened onChange={setPopoverOpen} />
            <div className="notif-head">
              <strong>{t.notifications}</strong>
            </div>
            {list.length === 0 ? (
              <p className="muted notif-empty">{t.caughtUp}</p>
            ) : (
              <div className="notif-list">
                {list.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    role="menuitem"
                    className="notif-line"
                    data-unread={fresh.has(item.id) ? "" : undefined}
                    onClick={() => (close(false), open(item))}
                  >
                    <span className="notif-dot" aria-hidden="true" />
                    <span className="notif-icon" aria-hidden="true">
                      <Icon name={kindIcons[item.kind] ?? "dot"} />
                    </span>
                    <span className="notif-line-text">
                      {fresh.has(item.id) && <span className="sr-only">{t.new}</span>}
                      {item.title}
                    </span>
                    <time className="notif-time" dateTime={item.created_at}>
                      {shortAgo(item.created_at)}
                    </time>
                  </button>
                ))}
              </div>
            )}
            <button
              type="button"
              role="menuitem"
              className="notif-footer"
              onClick={() => (close(false), navigate("/notifications"))}
            >
              {t.all}
            </button>
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

// Marks everything read while the list is on screen, but remembers what was unread so it stays highlighted until then.
function useMarkSeen(items: Notification[] | undefined, active: boolean, pending: boolean, markAll: () => void) {
  const [fresh, setFresh] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (!active) return setFresh((current) => (current.size ? new Set() : current));
    const unseen = (items ?? []).filter((item) => !item.read_at).map((item) => item.id);
    if (!unseen.length && !pending) return;
    setFresh((current) => new Set([...current, ...unseen]));
    markAll();
  }, [active, items, pending]);
  return fresh;
}

function Opened({ onChange }: { onChange: (open: boolean) => void }) {
  useEffect(() => {
    onChange(true);
    return () => onChange(false);
  }, []);
  return null;
}

function shortAgo(value: string) {
  const t = text[lang()];
  const minutes = Math.floor((Date.now() - new Date(value).getTime()) / 60_000);
  if (minutes < 1) return t.now;
  if (minutes < 60) return lang() === "es" ? `hace ${minutes} min` : `${minutes}m`;
  if (minutes < 1440) return lang() === "es" ? `hace ${Math.floor(minutes / 60)} h` : `${Math.floor(minutes / 60)}h`;
  return lang() === "es" ? `hace ${Math.floor(minutes / 1440)} d` : `${Math.floor(minutes / 1440)}d`;
}

function plain(markdown: string) {
  return markdown.replace(/\[([^\]]*)\]\([^)]*\)/g, "$1");
}

function Toast({ item, onOpen, onClose }: { item: Notification; onOpen: () => void; onClose: () => void }) {
  const t = useText(text);
  useEffect(() => {
    const timer = setTimeout(onClose, 6000);
    return () => clearTimeout(timer);
  }, []);
  return (
    <div className="notif-toast">
      <button type="button" className="notif-row" onClick={onOpen}>
        <RowBody item={item} body={item.body && <span className="notif-body">{plain(item.body)}</span>} />
      </button>
      <IconButton type="button" className="ghost" icon="x" label={t.dismiss} onClick={onClose} />
    </div>
  );
}

function dayLabel(value: string) {
  const t = text[lang()];
  const day = new Date(value).toDateString();
  const today = new Date();
  if (day === today.toDateString()) return t.today;
  if (day === new Date(today.getTime() - 86_400_000).toDateString()) return t.yesterday;
  return new Date(value).toLocaleDateString(locale(), { weekday: "long", month: "short", day: "numeric" });
}

export function NotificationsPage({ refresh }: { refresh: number }) {
  const t = useText(text);
  const [items, setItems] = useState<Notification[]>();
  const [cursor, setCursor] = useState<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<unknown>();
  const first = useRef(true);
  const fresh = useMarkSeen(items, true, false, () => {
    setItems((list) => list?.map(markRead));
    api.post("/api/notifications/read-all", {}).catch(setError);
  });

  // The first load sets the cursor; later refreshes only merge in the newest page so loaded pages stay.
  useEffect(() => {
    api
      .get<Page>("/api/notifications?limit=50")
      .then((page) => {
        setError(undefined);
        if (first.current) {
          first.current = false;
          setItems(page.notifications);
          setCursor(page.next_cursor);
          return;
        }
        const ids = new Set(page.notifications.map((item) => item.id));
        setItems((list) => [...page.notifications, ...(list ?? []).filter((item) => !ids.has(item.id))]);
      })
      .catch(setError);
  }, [refresh]);

  function loadMore() {
    setLoadingMore(true);
    api
      .get<Page>(`/api/notifications?limit=50&cursor=${encodeURIComponent(cursor ?? "")}`)
      .then((page) => {
        setItems((list) => {
          const ids = new Set((list ?? []).map((item) => item.id));
          return [...(list ?? []), ...page.notifications.filter((item) => !ids.has(item.id))];
        });
        setCursor(page.next_cursor);
      })
      .catch(setError)
      .finally(() => setLoadingMore(false));
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
          <p className="eyebrow">{t.inbox}</p>
          <h1>{t.notifications}</h1>
        </div>
      </div>
      <p className="muted">
        {t.description}
      </p>
      <ErrorText error={error} />
      <div className="stack notif-groups">
        {items?.length === 0 && <p className="panel muted notif-page-empty">{t.empty}</p>}
        {groups.map(([label, list]) => (
          <section key={label} aria-label={label}>
            <h2 className="notif-day">{label}</h2>
            <ul className="panel notif-page-list">
              {list.map((item) => (
                <li key={item.id} className="notif-row" data-unread={fresh.has(item.id) ? "" : undefined}>
                  <RowBody
                    item={{ ...item, read_at: fresh.has(item.id) ? null : item.read_at }}
                    onOpen={() => openLink(item.link)}
                    body={
                      item.body && (
                        <div className="notif-body">
                          <Markdown text={item.body} />
                        </div>
                      )
                    }
                  />
                </li>
              ))}
            </ul>
          </section>
        ))}
        {cursor && (
          <button type="button" className="notif-more" onClick={loadMore} disabled={loadingMore}>
            {loadingMore ? t.loading : t.more}
          </button>
        )}
      </div>
    </div>
  );
}
