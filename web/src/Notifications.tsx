import { useEffect, useRef, useState } from "react";
import Menu from "./Menu";
import { Icon, IconButton } from "./icons";
import { api } from "./api";
import { navigate } from "./App";
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

export default function Notifications({ refresh, onScreen }: { refresh: number; onScreen: (link: string) => boolean }) {
  const [list, setList] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const [toasts, setToasts] = useState<Notification[]>([]);
  const seen = useRef<Set<string>>(undefined);

  useEffect(() => {
    api
      .get<{ notifications: Notification[]; unread: number }>("/api/notifications")
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
    setList((current) => current.map((x) => (x.id === item.id ? { ...x, read_at: new Date().toISOString() } : x)));
    setUnread((count) => Math.max(0, count - 1));
    api.post(`/api/notifications/${item.id}/read`, {}).catch(() => undefined);
  }

  function readAll() {
    setList((current) => current.map((x) => x.read_at ? x : { ...x, read_at: new Date().toISOString() }));
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
              {unread > 0 && (
                <button type="button" role="menuitem" className="ghost" onClick={readAll}>
                  Mark all read
                </button>
              )}
            </div>
            {list.length === 0 && <p className="muted notif-empty">No notifications</p>}
            <div className="notif-list">
              {list.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  role="menuitem"
                  className="notif-item"
                  data-unread={item.read_at ? undefined : ""}
                  onClick={() => (close(false), open(item))}
                >
                  {!item.read_at && <span className="sr-only">Unread: </span>}
                  <span className="notif-title">{item.title}</span>
                  {item.body && <span className="notif-body">{item.body}</span>}
                  <small className="muted">{formatAgo(item.created_at)}</small>
                </button>
              ))}
            </div>
          </>
        )}
      </Menu>
      <div className="notif-toasts" role="status">
        {toasts.map((item) => (
          <Toast key={item.id} item={item} onOpen={() => open(item)} onClose={() => dismiss(item.id)} />
        ))}
      </div>
    </div>
  );
}

function Toast({ item, onOpen, onClose }: { item: Notification; onOpen: () => void; onClose: () => void }) {
  useEffect(() => {
    const timer = setTimeout(onClose, 6000);
    return () => clearTimeout(timer);
  }, []);
  return (
    <div className="notif-toast">
      <button type="button" className="notif-item" onClick={onOpen}>
        <span className="notif-title">{item.title}</span>
        {item.body && <span className="notif-body">{item.body}</span>}
      </button>
      <IconButton className="ghost" icon="x" label="Dismiss notification" onClick={onClose} />
    </div>
  );
}
