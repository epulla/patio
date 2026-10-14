import { confirmDialog } from "./confirm";
import {
  memo,
  useEffect,
  useId,
  useRef,
  useState,
  type ClipboardEvent,
  type CSSProperties,
  type DragEvent,
  type FormEvent,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import Markdown from "./Markdown";
import Menu from "./Menu";
import {
  api,
  type AllowedFolder,
  type Chat,
  type ChatApproval,
  type ChatAttachment,
  type ChatDefaults,
  type ChatMessage,
  type Connector,
} from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";
import SplitButton from "./SplitButton";
import Switch from "./Switch";
import { formatBytes } from "./Settings";
import { ConnectorRequired } from "./Connectors";
import { maxPins, pinLayout, setPins, usePins } from "./pins";

type ChatDetail = {
  chat: Chat;
  messages: ChatMessage[];
  pending_approvals: ChatApproval[];
  auto_approvals?: ChatApproval[];
};
type ChatEvent =
  | { type: "delta"; text: string }
  | { type: "thinking_delta"; text: string }
  | { type: "tool" | "thinking"; message: ChatMessage }
  | { type: "done"; message?: ChatMessage }
  | { type: "error"; message: string }
  | { type: "approval" };

type ModelOption = { id: string; label: string; description?: string };

export const lateTitle = "The app was closed at the scheduled time, so this ran when it opened again.";

const fallbackModels: ModelOption[] = [{ id: "", label: "Default" }];
const modelCache = new Map<string, ModelOption[]>();

export function useModels(connectorID: string | null | undefined) {
  const cached = connectorID ? modelCache.get(connectorID) : fallbackModels;
  const [state, setState] = useState({ id: connectorID, models: cached ?? fallbackModels, loading: !cached });
  useEffect(() => {
    if (!connectorID) return setState({ id: connectorID, models: fallbackModels, loading: false });
    const hit = modelCache.get(connectorID);
    if (hit) return setState({ id: connectorID, models: hit, loading: false });
    let active = true;
    setState({ id: connectorID, models: fallbackModels, loading: true });
    api
      .get<{ models: ModelOption[] }>(`/api/connectors/${connectorID}/models`)
      .then((result) => {
        const models = result.models.length ? result.models : fallbackModels;
        modelCache.set(connectorID, models);
        if (active) setState({ id: connectorID, models, loading: false });
      })
      .catch(() => active && setState({ id: connectorID, models: fallbackModels, loading: false }));
    return () => {
      active = false;
    };
  }, [connectorID]);
  return state.id === connectorID ? state : { models: fallbackModels, loading: true };
}

function MessageTime({ iso }: { iso: string }) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  const now = new Date();
  const time: Intl.DateTimeFormatOptions = { hour: "numeric", minute: "2-digit" };
  const options: Intl.DateTimeFormatOptions =
    date.toDateString() === now.toDateString()
      ? time
      : { month: "short", day: "numeric", ...(date.getFullYear() !== now.getFullYear() && { year: "numeric" }), ...time };
  return (
    <time className="message-time" dateTime={iso} title={date.toLocaleString()}>
      {date.toLocaleString(undefined, options)}
    </time>
  );
}

export function ModelOptions({ models, current, loading }: { models: ModelOption[]; current: string; loading: boolean }) {
  if (loading) return <option value={current}>Loading models…</option>;
  return (
    <>
      {models.map((m) => (
        <option key={m.id} value={m.id}>
          {m.label}
          {m.description ? ` · ${m.description}` : ""}
        </option>
      ))}
      {current && !models.some((m) => m.id === current) && <option value={current}>{current}</option>}
    </>
  );
}

type ChatPage = { chats: Chat[]; next_cursor: string | null };

const pageSize = 50;
const paneType = "application/x-chat-pane";

// Remounted per page (key) so a still-visible sentinel fires again.
function LoadMore({ onVisible }: { onVisible: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const callback = useRef(onVisible);
  callback.current = onVisible;
  useEffect(() => {
    const observer = new IntersectionObserver(([entry]) => entry.isIntersecting && callback.current());
    if (ref.current) observer.observe(ref.current);
    return () => observer.disconnect();
  }, []);
  return <div ref={ref} className="chat-list-end" aria-hidden="true" />;
}

export default function Chats({ id, refresh, windowed }: { id?: string; refresh: number; windowed?: boolean }) {
  const [chats, setChats] = useState<Chat[]>([]);
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);
  const pins = usePins();
  const [listOpen, setListOpen] = useState(() => localStorage.getItem("chat-list") !== "closed");
  const [cursor, setCursor] = useState<string | null>(null);
  const loadedCount = useRef(0);
  const generation = useRef(0);
  const loadingMore = useRef(false);
  const pinsRef = useRef(pins);
  const existingPins = useRef(new Set<string>());
  // The rail scrolls, which would clip a CSS [data-tip] tooltip, so it is positioned fixed instead.
  const [tip, setTip] = useState<{ text: string; top: number; left: number; focus: boolean }>();

  function showTip(button: HTMLElement, text: string, focus: boolean) {
    if (focus && !button.matches(":focus-visible")) return;
    const rect = button.getBoundingClientRect();
    setTip({ text, top: rect.top + rect.height / 2, left: rect.right + 8, focus });
  }

  useEffect(() => {
    const current = ++generation.current;
    Promise.all([
      api.get<ChatPage>(`/api/chats?limit=${Math.max(pageSize, loadedCount.current)}`),
      api.get<Connector[]>("/api/connectors"),
    ])
      .then(([page, connectorList]) => {
        if (current !== generation.current) return;
        setChats(page.chats);
        setCursor(page.next_cursor);
        setConnectors(connectorList);
        setLoaded(true);
        setError(undefined);
        const listed = new Set(page.chats.map((chat) => chat.id));
        for (const pinned of pinsRef.current.ids) {
          if (listed.has(pinned) || existingPins.current.has(pinned)) continue;
          api
            .get(`/api/chats/${pinned}`)
            .then(() => existingPins.current.add(pinned))
            .catch((err: unknown) => {
              if (!(err instanceof Error) || err.message !== "chat not found") return;
              setPins((pins) => (pins.ids.includes(pinned) ? pinLayout(pins.ids.filter((x) => x !== pinned)) : pins));
            });
        }
      })
      .catch(setError);
  }, [refresh, reload, id]);

  useEffect(() => {
    loadedCount.current = chats.length;
  }, [chats]);

  useEffect(() => {
    pinsRef.current = pins;
  }, [pins]);

  async function loadMore() {
    if (!cursor || loadingMore.current) return;
    loadingMore.current = true;
    const current = generation.current;
    try {
      const page = await api.get<ChatPage>(`/api/chats?limit=${pageSize}&cursor=${encodeURIComponent(cursor)}`);
      if (current !== generation.current) return;
      setChats((chats) => {
        const seen = new Set(chats.map((chat) => chat.id));
        return [...chats, ...page.chats.filter((chat) => !seen.has(chat.id))];
      });
      setCursor(page.next_cursor);
    } catch (err) {
      setError(err);
    } finally {
      loadingMore.current = false;
    }
  }

  function togglePin(chatID: string) {
    if (pins.ids.includes(chatID)) return setPins(pinLayout(pins.ids.filter((pinned) => pinned !== chatID)));
    if (pins.ids.length >= maxPins) return setError(new Error(`Unpin a chat first (max ${maxPins}).`));
    setPins(pinLayout([...pins.ids, chatID]));
  }

  function swap(from: number, to: number) {
    const ids = [...pins.ids];
    [ids[from], ids[to]] = [ids[to], ids[from]];
    setPins({ ...pins, ids });
  }

  const showGrid = !windowed && pins.ids.length > 0 && (!id || pins.ids.includes(id));
  const tracks = (sizes: number[]) => sizes.map((size) => `minmax(240px, ${size}fr)`).join(" 6px ");

  return (
    <div className={windowed ? "chats windowed" : "chats"}>
      {!windowed && (
      <aside className={listOpen ? "chat-list" : "chat-list collapsed"} aria-label="Chats">
        <div className="chat-list-head">
          {listOpen && (connectors.length > 0 || !loaded) && (
            <button className="primary" onClick={() => setCreating(true)}>
              <Icon name="plus" /> New chat
            </button>
          )}
          <IconButton
            className="ghost"
            icon={listOpen ? "chevronsLeft" : "chevronsRight"}
            label={
              listOpen
                ? "Collapse chat list"
                : loaded && chats.length === 0 && !error
                  ? "Expand chat list (no chats yet)"
                  : "Expand chat list"
            }
            aria-expanded={listOpen}
            onClick={() => {
              localStorage.setItem("chat-list", listOpen ? "closed" : "open");
              setListOpen(!listOpen);
              setTip(undefined);
            }}
          />
        </div>
        {!listOpen && (connectors.length > 0 || !loaded) && (
          <IconButton className="ghost" icon="plus" label="New chat" onClick={() => setCreating(true)} />
        )}
        {!listOpen && pins.ids.length > 0 && (
          <IconButton
            className="ghost"
            icon="boards"
            label={`Pinned (${pins.ids.length})`}
            aria-current={showGrid ? "page" : undefined}
            onClick={() => navigate("/ai/chats")}
          />
        )}
        {!listOpen && chats.length > 0 && (
          <nav className="chat-rail" onScroll={() => setTip(undefined)}>
            {chats.map((chat) => {
              const pinned = pins.ids.includes(chat.id);
              const scheduled = !!chat.scheduled_task_id;
              return (
                <button
                  key={chat.id}
                  className="chat-initial"
                  aria-label={`${chat.title}${scheduled ? ", scheduled" : ""}${pinned ? ", pinned" : ""}`}
                  aria-current={!showGrid && chat.id === id ? "page" : undefined}
                  onClick={() => navigate(`/ai/chats/${chat.id}`)}
                  onMouseEnter={(event) => showTip(event.currentTarget, chat.title, false)}
                  onFocus={(event) => showTip(event.currentTarget, chat.title, true)}
                  onMouseLeave={() => setTip(undefined)}
                  onBlur={() => setTip(undefined)}
                >
                  {[...chat.title.trim()][0]?.toUpperCase() ?? "?"}
                  {(scheduled || pinned) && (
                    <span className="chat-badges" aria-hidden="true">
                      {scheduled && (
                        <span className="badge-scheduled">
                          <Icon name="clock" />
                        </span>
                      )}
                      {pinned && (
                        <span className="badge-pinned">
                          <Icon name="pin" />
                        </span>
                      )}
                    </span>
                  )}
                </button>
              );
            })}
            {cursor && <LoadMore key={chats.length} onVisible={loadMore} />}
          </nav>
        )}
        {tip && (
          <div className={tip.focus ? "rail-tip focus" : "rail-tip"} aria-hidden="true" style={{ top: tip.top, left: tip.left }}>
            {tip.text}
          </div>
        )}
        <ErrorText error={error} />
        {listOpen && chats.length === 0 && !error && <p className="muted">No chats yet.</p>}
        {listOpen && (
        <nav>
          {pins.ids.length > 0 && (
            <button
              className="chat-link"
              aria-current={showGrid ? "page" : undefined}
              onClick={() => navigate("/ai/chats")}
            >
              <span className="chat-link-pinned">
                <Icon name="boards" /> Pinned ({pins.ids.length})
              </span>
            </button>
          )}
          {chats.map((chat) => {
            const pinned = pins.ids.includes(chat.id);
            return (
              <div key={chat.id} className="chat-row">
                <button
                  className="chat-link"
                  aria-current={!showGrid && chat.id === id ? "page" : undefined}
                  onClick={() => navigate(`/ai/chats/${chat.id}`)}
                >
                  <span>{chat.title}</span>
                  <small>
                    {new Date(chat.updated_at).toLocaleDateString()}
                    {chat.scheduled_task_id && (
                      <>
                        {" · "}
                        <Icon name="clock" /> Scheduled
                      </>
                    )}
                    {chat.scheduled_late && (
                      <span className="run-label" title={lateTitle}>
                        Ran late
                      </span>
                    )}
                  </small>
                </button>
                <IconButton
                  className={pinned ? "ghost pinned" : "ghost"}
                  icon="pin"
                  label={pinned ? "Unpin chat" : "Pin chat"}
                  onClick={() => togglePin(chat.id)}
                />
              </div>
            );
          })}
          {cursor && <LoadMore key={chats.length} onVisible={loadMore} />}
        </nav>
        )}
      </aside>
      )}
      {showGrid ? (
        <div className="chat-grid" style={{ gridTemplateColumns: tracks(pins.cols), gridTemplateRows: tracks(pins.rows) }}>
          {pins.ids.map((chatID, index) => (
            <div
              key={chatID}
              className="grid-pane"
              style={{
                gridColumn: (index % pins.cols.length) * 2 + 1,
                gridRow: Math.floor(index / pins.cols.length) * 2 + 1,
              }}
              onDragOver={(event) => {
                if (event.dataTransfer.types.includes(paneType)) event.preventDefault();
              }}
              onDrop={(event) => {
                const from = event.dataTransfer.getData(paneType);
                if (!from) return;
                event.preventDefault();
                swap(Number(from), index);
              }}
            >
              <Conversation
                id={chatID}
                refresh={refresh}
                connectors={connectors}
                pinned
                focused={chatID === id}
                onPin={() => togglePin(chatID)}
                onChanged={() => setReload((value) => value + 1)}
                pane={
                  <>
                    <span
                      className="pane-grip"
                      draggable
                      aria-hidden="true"
                      data-tip="Drag to swap"
                      onDragStart={(event) => {
                        event.dataTransfer.setData(paneType, String(index));
                        event.dataTransfer.effectAllowed = "move";
                      }}
                    >
                      <Icon name="grip" />
                    </span>
                    {pins.ids.length > 1 && (
                      <Menu label="Move chat" buttonClassName="icon ghost" button={<Icon name="more" />}>
                        {(close) => (
                          <>
                            {index > 0 && (
                              <button role="menuitem" onClick={() => (close(), swap(index, index - 1))}>
                                <Icon name="chevronLeft" /> Move earlier
                              </button>
                            )}
                            {index < pins.ids.length - 1 && (
                              <button role="menuitem" onClick={() => (close(), swap(index, index + 1))}>
                                <Icon name="chevronRight" /> Move later
                              </button>
                            )}
                          </>
                        )}
                      </Menu>
                    )}
                  </>
                }
              />
            </div>
          ))}
          {pins.cols.slice(1).map((_, index) => (
            <Splitter
              key={`col-${index}`}
              vertical
              label="Resize columns"
              sizes={pins.cols}
              index={index}
              min={240}
              style={{ gridColumn: index * 2 + 2, gridRow: "1 / -1" }}
              onChange={(cols) => setPins((current) => ({ ...current, cols }))}
            />
          ))}
          {pins.rows.slice(1).map((_, index) => (
            <Splitter
              key={`row-${index}`}
              label="Resize rows"
              sizes={pins.rows}
              index={index}
              min={240}
              style={{ gridRow: index * 2 + 2, gridColumn: "1 / -1" }}
              onChange={(rows) => setPins((current) => ({ ...current, rows }))}
            />
          ))}
        </div>
      ) : id ? (
        <Conversation
          key={id}
          id={id}
          refresh={refresh}
          windowed={windowed}
          connectors={connectors}
          pinned={pins.ids.includes(id)}
          onPin={windowed ? undefined : () => togglePin(id)}
          onChanged={() => setReload((value) => value + 1)}
        />
      ) : connectors.length || !loaded ? (
        <section className="chat-empty">
          <h1>Chats</h1>
          <p className="muted">Pick a chat or start a new one.</p>
        </section>
      ) : (
        <ConnectorRequired then="start a chat" />
      )}
      {creating && (
        <NewChatDialog
          connectors={connectors}
          onClose={() => setCreating(false)}
          onCreated={(chat) => {
            setCreating(false);
            navigate(`/ai/chats/${chat.id}`);
          }}
        />
      )}
    </div>
  );
}

function Splitter({
  label,
  vertical,
  sizes,
  index,
  min,
  style,
  onChange,
}: {
  label: string;
  vertical?: boolean;
  sizes: number[];
  index: number;
  min: number;
  style?: CSSProperties;
  onChange: (sizes: number[]) => void;
}) {
  function move(element: HTMLElement, start: number[], pixels: number) {
    const box = element.parentElement?.getBoundingClientRect();
    if (!box) return;
    const limit = min / (vertical ? box.width : box.height);
    if (start[index] + start[index + 1] < limit * 2) return;
    const delta = Math.min(Math.max(pixels / (vertical ? box.width : box.height), limit - start[index]), start[index + 1] - limit);
    const next = [...start];
    next[index] += delta;
    next[index + 1] -= delta;
    onChange(next);
  }

  return (
    <div
      role="separator"
      tabIndex={0}
      aria-label={label}
      aria-orientation={vertical ? "vertical" : "horizontal"}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round((sizes[index] / (sizes[index] + sizes[index + 1])) * 100)}
      className="splitter"
      style={style}
      onPointerDown={(event) => {
        event.preventDefault();
        const element = event.currentTarget;
        const start = sizes;
        const origin = vertical ? event.clientX : event.clientY;
        element.setPointerCapture(event.pointerId);
        element.onpointermove = (moved) => move(element, start, (vertical ? moved.clientX : moved.clientY) - origin);
        element.onlostpointercapture = () => (element.onpointermove = null);
      }}
      onKeyDown={(event) => {
        const steps: Record<string, number> = vertical ? { ArrowLeft: -1, ArrowRight: 1 } : { ArrowUp: -1, ArrowDown: 1 };
        const step = steps[event.key];
        if (!step) return;
        event.preventDefault();
        move(event.currentTarget, sizes, step * 24);
      }}
    />
  );
}

export function Conversation({
  id,
  refresh,
  windowed,
  compact,
  otherWindow,
  connectors,
  pinned,
  focused,
  pane,
  onPin,
  onChanged,
}: {
  id: string;
  refresh: number;
  windowed?: boolean;
  compact?: boolean;
  otherWindow?: boolean;
  connectors: Connector[];
  pinned?: boolean;
  focused?: boolean;
  pane?: ReactNode;
  onPin?: () => void;
  onChanged: () => void;
}) {
  const uid = useId();
  const [split, setSplit] = useState<"" | "row" | "column">("");
  const [choosingFolder, setChoosingFolder] = useState(false);
  const [splitSizes, setSplitSizes] = useState([0.5, 0.5]);
  const input = useRef<HTMLTextAreaElement>(null);
  const [detail, setDetail] = useState<ChatDetail>();
  const [streaming, setStreaming] = useState("");
  const [thinking, setThinking] = useState("");
  const [running, setRunning] = useState(false);
  const [text, setText] = useState("");
  const [title, setTitle] = useState("");
  const [error, setError] = useState<unknown>();
  const [missing, setMissing] = useState(false);
  const [announcement, setAnnouncement] = useState("");
  const [pending, setPending] = useState<ChatAttachment[]>([]);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const end = useRef<HTMLDivElement>(null);
  const filePicker = useRef<HTMLInputElement>(null);
  const { models, loading: modelsLoading } = useModels(detail?.chat.connector_id);

  const load = () =>
    api
      .get<ChatDetail>(`/api/chats/${id}`)
      .then((value) => {
        setDetail(value);
        setTitle(value.chat.title);
        setRunning(value.chat.running);
      })
      .catch((reason) => {
        if (String(reason).includes("not found")) setMissing(true);
        else setError(reason);
      });

  useEffect(() => {
    void load();
  }, [id, refresh]);

  useEffect(() => {
    setPending([]);
    setDragging(false);
  }, [id]);

  useEffect(() => {
    const unsubscribe = window.runtime?.EventsOn(`chat:${id}`, (data) => {
      const event = data as ChatEvent;
      if (event.type === "approval") void load();
      if (event.type === "delta") setStreaming((current) => current + event.text);
      if (event.type === "thinking_delta") setThinking((current) => current + event.text);
      if (event.type === "thinking") setThinking("");
      if (event.type === "tool" || event.type === "thinking" || event.type === "done") {
        if (event.type === "done") {
          setStreaming("");
          setThinking("");
          setRunning(false);
          setAnnouncement("Reply received.");
          void load();
          return;
        }
        const message = event.message;
        if (message)
          setDetail((current) =>
            current && !current.messages.some((m) => m.id === message.id)
              ? { ...current, messages: [...current.messages, message] }
              : current,
          );
      }
      if (event.type === "error") {
        setStreaming("");
        setThinking("");
        setRunning(false);
        setError(event.message);
      }
    });
    return () => unsubscribe?.();
  }, [id]);

  useEffect(() => {
    const box = end.current?.parentElement;
    if (box) box.scrollTop = box.scrollHeight;
  }, [detail?.messages.length, streaming, detail?.pending_approvals.length, split]);

  const ready = !!detail;
  useEffect(() => {
    if (focused && ready) input.current?.focus();
  }, [focused, ready]);

  async function attach(files: File[]) {
    if (!files.length || uploading) return;
    const problem = attachmentProblem(files, pending.length);
    if (problem) {
      setError(new Error(problem));
      return;
    }
    setError(undefined);
    setUploading(true);
    try {
      for (const file of files) {
        const added = await api.upload<ChatAttachment[]>(`/api/chats/${id}/attachments`, file);
        setPending((current) => [...current, ...added]);
      }
    } catch (reason) {
      setError(reason);
    } finally {
      setUploading(false);
    }
  }

  async function unattach(attachment: ChatAttachment) {
    try {
      await api.delete(`/api/chats/${id}/attachments/${encodeURIComponent(attachment.name)}`);
      setPending((current) => current.filter((a) => a.name !== attachment.name));
    } catch (reason) {
      setError(reason);
    }
  }

  async function revealAttachment(attachment: ChatAttachment) {
    try {
      await api.post(`/api/chats/${id}/attachments/${encodeURIComponent(attachment.name)}/reveal`, {});
    } catch (reason) {
      setError(reason);
    }
  }

  function onPaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    const files = Array.from(event.clipboardData.files);
    if (!files.length) return;
    event.preventDefault();
    void attach(files.map(pastedFile));
  }

  function onDragOver(event: DragEvent) {
    if (!connector || !event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    setDragging(true);
  }

  function onDragLeave(event: DragEvent) {
    const box = event.currentTarget.getBoundingClientRect();
    const inside =
      event.clientX > box.left && event.clientX < box.right && event.clientY > box.top && event.clientY < box.bottom;
    if (!inside) setDragging(false);
  }

  function onDrop(event: DragEvent) {
    if (!event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    event.stopPropagation();
    setDragging(false);
    if (connector) void attach(Array.from(event.dataTransfer.files));
  }

  async function send(event?: FormEvent) {
    event?.preventDefault();
    const value = text.trim();
    if ((!value && !pending.length) || running || uploading || !detail) return;
    setError(undefined);
    setRunning(true);
    try {
      const message = await api.post<ChatMessage>(`/api/chats/${id}/messages`, {
        text: value,
        ...(pending.length ? { attachments: pending.map((a) => a.name) } : {}),
      });
      setText("");
      setPending([]);
      setDetail((current) =>
        current && !current.messages.some((m) => m.id === message.id)
          ? { ...current, messages: [...current.messages, message] }
          : current,
      );
      if (detail.chat.title === "New chat" && detail.messages.length === 0) {
        const label = value || pending.map((a) => a.name).join(", ");
        await rename(label.length > 48 ? `${label.slice(0, 48)}…` : label);
      }
    } catch (reason) {
      setRunning(false);
      setError(reason);
    }
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      void send();
    }
  }

  async function stop() {
    try {
      await api.post(`/api/chats/${id}/stop`, {});
    } catch (reason) {
      setError(reason);
    }
  }

  async function rename(next: string) {
    const value = next.trim();
    if (!value || value === detail?.chat.title) return setTitle(detail?.chat.title ?? "");
    try {
      await api.put(`/api/chats/${id}`, { title: value });
      setTitle(value);
      setDetail((current) => current && { ...current, chat: { ...current.chat, title: value } });
      onChanged();
    } catch (reason) {
      setError(reason);
    }
  }

  async function update(
    fields: { model: string } | { thinking: boolean } | { show_thinking: boolean } | { auto_allow: boolean },
  ) {
    try {
      await api.put(`/api/chats/${id}`, fields);
      setDetail((current) => current && { ...current, chat: { ...current.chat, ...fields } });
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  async function changeAutoAllow(on: boolean) {
    if (on && !(await confirmDialog("Allow every file change and command in this chat without asking? You can turn it off anytime.", "Turn on auto-allow"))) return;
    await update({ auto_allow: on });
    if (on) void load();
  }

  async function changeFolder(folder: string | null) {
    const chat = await api.put<Chat>(`/api/chats/${id}`, { folder });
    setDetail((current) => current && { ...current, chat: { ...current.chat, ...chat } });
    setChoosingFolder(false);
    setError(undefined);
  }

  async function reveal(path: string) {
    try {
      await api.post(`/api/chats/${id}/reveal`, { path });
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove() {
    if (!(await confirmDialog(`Delete chat "${detail?.chat.title}"?`))) return;
    try {
      await api.delete(`/api/chats/${id}`);
      onChanged();
      navigate("/ai/chats");
    } catch (reason) {
      setError(reason);
    }
  }

  async function answer(approval: ChatApproval, allow: boolean) {
    try {
      await api.post(`/api/chats/${id}/approvals/${approval.id}`, { allow });
      setDetail(
        (current) =>
          current && {
            ...current,
            pending_approvals: current.pending_approvals.filter((a) => a.id !== approval.id),
          },
      );
    } catch (reason) {
      setError(reason);
      void load();
    }
  }

  if (missing)
    return (
      <section className="chat-empty">
        <p className="muted">This chat no longer exists.</p>
      </section>
    );
  if (!detail)
    return (
      <section className="chat-empty" aria-busy="true" aria-label="Loading chat">
        <p role="status">Loading chat…</p>
      </section>
    );

  const connector = connectors.find((c) => c.id === detail.chat.connector_id);
  const folderLabel = detail.chat.folder ?? "Scratch folder (no files shared)";
  const template = `minmax(0, ${splitSizes[0]}fr) 6px minmax(0, ${splitSizes[1]}fr)`;
  const history = (
    <>
      {detail.messages.length === 0 && !streaming && (
        <p className="muted chat-hint">
          Ask anything. The assistant can read files in this chat's folder and use your tickets. Attach text files
          and images with the paperclip, drag and drop, or paste.
        </p>
      )}
      <Timeline chatId={id} detail={detail} onError={setError} />
      {thinking && detail.chat.show_thinking && <ThinkingBlock text={thinking} live />}
      {streaming && (
        <div className="message assistant">
          <Markdown text={streaming} onFile={reveal} />
        </div>
      )}
      {running && !streaming && (
        <p className="muted typing">
          Working<span className="typing-dots" aria-hidden="true" />
        </p>
      )}
      {detail.pending_approvals.map((approval) => (
        <ApprovalCard key={approval.id} approval={approval} onAnswer={(allow) => answer(approval, allow)} />
      ))}
    </>
  );

  return (
    <section
      className={dragging ? "conversation dragging" : "conversation"}
      aria-labelledby={`${uid}-title`}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
      {!compact && (
      <header className="conversation-head">
        <div className="conversation-title">
          <label className="sr-only" htmlFor={`${uid}-title`}>
            Chat title
          </label>
          <input
            id={`${uid}-title`}
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            onBlur={(event) => rename(event.target.value)}
            onKeyDown={(event) => event.key === "Enter" && event.currentTarget.blur()}
          />
        </div>
        {pane}
        {onPin && (
          <IconButton
            className={pinned ? "ghost pinned" : "ghost"}
            icon="pin"
            label={pinned ? "Unpin chat" : "Pin chat"}
            onClick={onPin}
          />
        )}
        {!pane &&
          (split ? (
            <IconButton className="ghost" icon="square" label="Close split" onClick={() => setSplit("")} />
          ) : (
            <SplitButton icon="columns" label="Split side by side" menuLabel="Split options" onClick={() => setSplit("row")}>
              {(close) => (
                <>
                  <button role="menuitem" onClick={() => (close(), setSplit("row"))}>
                    <Icon name="columns" /> Side by side
                  </button>
                  <button role="menuitem" onClick={() => (close(), setSplit("column"))}>
                    <Icon name="rows" /> Stacked
                  </button>
                </>
              )}
            </SplitButton>
          ))}
        <IconButton
          className="ghost"
          icon={windowed ? "arrowLeft" : "externalLink"}
          label={windowed ? "Open main window" : "Open in new window"}
          onClick={() =>
            api
              .post(windowed ? "/api/windows/main" : `/api/chats/${id}/window`, windowed ? { path: `/ai/chats/${id}` } : {})
              .catch(setError)
          }
        />
        <IconButton className="danger-text" icon="trash" label="Delete chat" onClick={remove} />
        {detail.chat.scheduled_task_id && (
          <div className="conversation-meta">
            <button
              type="button"
              className="folder-chip"
              onClick={() => navigate(`/ai/scheduled/${detail.chat.scheduled_task_id}`)}
            >
              <Icon name="clock" /> Scheduled task
            </button>
          </div>
        )}
      </header>
      )}
      {choosingFolder && (
        <FolderDialog
          current={detail.chat.folder}
          onPick={changeFolder}
          onClose={() => setChoosingFolder(false)}
          onSettings={() =>
            windowed ? api.post("/api/windows/main", { path: "/settings" }).catch(setError) : navigate("/settings")
          }
        />
      )}

      <p className="sr-only" role="status">
        {running ? "Assistant is replying." : announcement}
      </p>
      {split ? (
        <div
          className="split"
          style={split === "row" ? { gridTemplateColumns: template } : { gridTemplateRows: template }}
        >
          <div className="messages">{history}</div>
          <Splitter
            vertical={split === "row"}
            label="Resize split"
            sizes={splitSizes}
            index={0}
            min={120}
            onChange={setSplitSizes}
          />
          <div className="messages">
            {history}
            <div ref={end} />
          </div>
        </div>
      ) : (
        <div className="messages">
          {history}
          <div ref={end} />
        </div>
      )}

      <ErrorText error={error} />
      {otherWindow && (
        <p className="muted dock-note">
          Replying in another window.
          <button type="button" className="ghost" onClick={() => api.post(`/api/chats/${id}/window`, {}).catch(setError)}>
            Open in its window
          </button>
        </p>
      )}
      {dragging && (
        <p className="drop-hint" aria-hidden="true">
          <Icon name="paperclip" /> Drop files to attach
        </p>
      )}
      {(pending.length > 0 || uploading) && (
        <ul className="attachments" aria-label="Files to send">
          {pending.map((attachment) => (
            <li key={attachment.name} className="attachment">
              <AttachmentLabel attachment={attachment} />
              <IconButton
                type="button"
                icon="x"
                label={`Remove ${attachment.name}`}
                disabled={running}
                onClick={() => unattach(attachment)}
              />
            </li>
          ))}
          {uploading && (
            <li className="muted" role="status">
              Uploading…
            </li>
          )}
        </ul>
      )}
      <form className="composer" onSubmit={send}>
        <input
          ref={filePicker}
          type="file"
          multiple
          hidden
          onChange={(event) => {
            void attach(Array.from(event.target.files ?? []));
            event.target.value = "";
          }}
        />
        <label className="sr-only" htmlFor={`${uid}-input`}>
          Message
        </label>
        <textarea
          ref={input}
          id={`${uid}-input`}
          rows={compact ? 2 : 3}
          placeholder={connector ? "Message… (Enter to send, Shift+Enter for a new line)" : "This chat's connector was removed"}
          disabled={!connector || otherWindow}
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          onDragOver={onDragOver}
          onDrop={onDrop}
        />
        <div className="composer-actions">
          <div className="composer-tools">
            <IconButton
              type="button"
              className="ghost tip-up"
              icon="paperclip"
              label="Attach files"
              disabled={!connector || uploading || pending.length >= maxAttachments}
              onClick={() => filePicker.current?.click()}
            />
            {detail.messages.length === 0 && !running ? (
              <span className="tip-up" data-tip={`Folder: ${folderLabel}`}>
                <Menu label={`Folder: ${folderLabel}`} buttonClassName="icon ghost" button={<Icon name="folder" />}>
                  {(close) => (
                    <>
                      <button role="menuitem" onClick={() => (close(false), setChoosingFolder(true))}>
                        <Icon name="folder" /> Change folder…
                      </button>
                      <button role="menuitem" onClick={() => (close(), reveal(detail.chat.workdir))}>
                        <Icon name="externalLink" /> Open in file manager
                      </button>
                    </>
                  )}
                </Menu>
              </span>
            ) : (
              <IconButton
                type="button"
                className="ghost tip-up tip-wide"
                icon="folder"
                label={`Open in file manager: ${folderLabel}. Start a new chat to use another folder.`}
                onClick={() => reveal(detail.chat.workdir)}
              />
            )}
            <span className="tip-up" data-tip="Chat settings">
              <Menu
                label={detail.chat.auto_allow ? "Chat settings (auto-allow is on)" : "Chat settings"}
                buttonClassName="icon ghost settings-trigger"
                button={
                  <>
                    <Icon name="settings" />
                    {detail.chat.auto_allow && <span className="settings-dot" aria-hidden="true" />}
                  </>
                }
              >
                {() => (
                  <div className="chat-settings">
                    <SettingRow
                      label="Thinking"
                      hint={thinkingTitle(detail.chat, running)}
                      checked={detail.chat.capabilities.thinking.supported && detail.chat.thinking}
                      disabled={running || !detail.chat.capabilities.thinking.supported}
                      onChange={(thinking) => update({ thinking })}
                    />
                    <SettingRow
                      label="Show thinking"
                      hint={
                        connector?.kind === "claude"
                          ? "Claude Code doesn't share its reasoning, so no thinking appears in this chat"
                          : connector?.kind === "codex"
                            ? "Show Codex's reasoning summaries when it sends them; simple questions often have none"
                            : "Show the model's reasoning above its replies"
                      }
                      checked={detail.chat.show_thinking}
                      onChange={(show_thinking) => update({ show_thinking })}
                    />
                    <SettingRow
                      label="Auto-allow"
                      warning
                      hint={
                        detail.chat.auto_allow
                          ? "Every file change and command runs without asking"
                          : "Ask before every file change and command"
                      }
                      checked={detail.chat.auto_allow}
                      onChange={changeAutoAllow}
                    />
                  </div>
                )}
              </Menu>
            </span>
          </div>
          <div className="composer-send">
            {!compact && (
            <label className="composer-model">
              <span className="muted">{connector ? connector.name : "Connector removed"}</span>
              <span className="sr-only">Model</span>
              <select
                value={detail.chat.model}
                disabled={!connector || running || modelsLoading}
                aria-busy={modelsLoading}
                onChange={(event) => update({ model: event.target.value })}
              >
                <ModelOptions models={models} current={detail.chat.model} loading={modelsLoading} />
              </select>
              {modelsLoading && <span className="sr-only" role="status">Loading models</span>}
            </label>
            )}
            {running ? (
              <button type="button" onClick={stop}>
                <span className="spinner" aria-hidden="true" /> Stop
              </button>
            ) : (
              <button className="primary" disabled={(!text.trim() && !pending.length) || uploading || !connector || otherWindow}>
                <Icon name="send" /> Send
              </button>
            )}
          </div>
        </div>
      </form>
    </section>
  );
}

const maxAttachments = 10;
const imageTypes = ["image/png", "image/jpeg", "image/gif", "image/webp"];

function attachmentProblem(files: File[], alreadyAttached: number) {
  if (alreadyAttached + files.length > maxAttachments) return `Attach at most ${maxAttachments} files per message.`;
  for (const file of files) {
    const image = imageTypes.includes(file.type);
    if (image && file.size > 10 << 20) return `${file.name} is larger than 10 MB.`;
    if (!image && file.size > 1 << 20) return `${file.name} is larger than 1 MB. Text files can be up to 1 MB.`;
  }
  return "";
}

function pastedFile(file: File) {
  if (!file.type.startsWith("image/") || (file.name && file.name !== "image.png")) return file;
  const now = new Date();
  const pad = (value: number) => String(value).padStart(2, "0");
  const stamp = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}.${pad(now.getMinutes())}.${pad(now.getSeconds())}`;
  const extension = file.type.split("/")[1]?.replace("jpeg", "jpg") || "png";
  return new File([file], `pasted-${stamp}.${extension}`, { type: file.type });
}

function AttachmentLabel({ attachment }: { attachment: ChatAttachment }) {
  return (
    <span className="attachment-label">
      <Icon name={attachment.kind === "image" ? "image" : "file"} />
      <span className="attachment-name">{attachment.name}</span>
      <span className="muted">{formatBytes(attachment.size)}</span>
    </span>
  );
}

function SettingRow({
  label,
  hint,
  checked,
  disabled,
  warning,
  onChange,
}: {
  label: string;
  hint: string;
  checked: boolean;
  disabled?: boolean;
  warning?: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <button
      type="button"
      role="menuitemcheckbox"
      aria-checked={checked}
      disabled={disabled}
      className={warning ? "setting-row warning" : "setting-row"}
      onClick={() => onChange(!checked)}
    >
      <span className="setting-text">
        <span>{label}</span>
        <small className="muted">{hint}</small>
      </span>
      <span className="switch-track" aria-hidden="true" />
    </button>
  );
}

function thinkingTitle(chat: Chat, running: boolean) {
  const { supported, reason } = chat.capabilities.thinking;
  if (!supported) return reason;
  if (running) return "Stop the reply to change thinking";
  return ["Let the model reason before answering. Slower but smarter.", reason].filter(Boolean).join(" ");
}

function ThinkingBlock({ text, live }: { text: string; live?: boolean }) {
  return (
    <details className="thinking">
      <summary>{live ? "Thinking…" : "Thinking"}</summary>
      <p>{text}</p>
    </details>
  );
}

function timeline(detail: ChatDetail): (ChatMessage | ChatApproval)[] {
  const auto = detail.auto_approvals ?? [];
  if (!auto.length) return detail.messages;
  return [...detail.messages, ...auto].sort((a, b) => a.created_at.localeCompare(b.created_at));
}

// Memoized so typing and streamed deltas don't re-render hundreds of past messages.
const Timeline = memo(function Timeline({
  chatId,
  detail,
  onError,
}: {
  chatId: string;
  detail: ChatDetail;
  onError: (reason: unknown) => void;
}) {
  async function reveal(path: string) {
    try {
      await api.post(`/api/chats/${chatId}/reveal`, { path });
      onError(undefined);
    } catch (reason) {
      onError(reason);
    }
  }
  async function revealAttachment(attachment: ChatAttachment) {
    try {
      await api.post(`/api/chats/${chatId}/attachments/${encodeURIComponent(attachment.name)}/reveal`, {});
    } catch (reason) {
      onError(reason);
    }
  }
  return (
    <>
    {timeline(detail).map((message) =>
      "tool_name" in message ? (
        <ToolLine key={message.id} body={() => `${message.tool_name}\n${prettyJSON(message.input_json)}`}>
          <Icon name="check" /> <code>Auto-allowed {approvalSummary(message)}</code>{" "}
          <MessageTime iso={message.created_at} />
        </ToolLine>
      ) : message.role === "tool" ? (
        <ToolLine key={message.id} body={() => prettyJSON(message.content)}>
          <Icon name="plug" /> <code>{message.content}</code> <MessageTime iso={message.created_at} />
        </ToolLine>
      ) : message.role === "thinking" ? (
        detail.chat.show_thinking && <ThinkingBlock key={message.id} text={message.content} />
      ) : (
        <div key={message.id} className={`message-row ${message.role}`}>
          {(message.role !== "user" || message.content) && (
            <div className={`message ${message.role}`}>
              {message.role === "assistant" || message.role === "user" ? (
                <Markdown text={message.content} onFile={reveal} />
              ) : (
                message.content
              )}
            </div>
          )}
          {message.attachments.length > 0 && (
            <ul className="attachments sent" aria-label="Attachments">
              {message.attachments.map((attachment) => (
                <li key={attachment.name}>
                  <button
                    type="button"
                    className={attachment.kind === "image" ? "attachment image" : "attachment"}
                    title="Show in folder"
                    onClick={() => revealAttachment(attachment)}
                  >
                    {attachment.kind === "image" && (
                      <img
                        src={`/api/chats/${chatId}/attachments/${encodeURIComponent(attachment.name)}`}
                        alt=""
                        loading="lazy"
                      />
                    )}
                    <AttachmentLabel attachment={attachment} />
                  </button>
                </li>
              ))}
            </ul>
          )}
          <MessageTime iso={message.created_at} />
        </div>
      ),
    )}
    </>
  );
});

// Formats the full input only when expanded; tool inputs can be 20k chars and chats have hundreds.
function ToolLine({ body, children }: { body: () => string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <details className="tool-line" onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>{children}</summary>
      {open && <pre>{body()}</pre>}
    </details>
  );
}

function prettyJSON(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    // Tool lines are stored as "<tool name> <json>".
    const space = text.indexOf(" ");
    return space > 0 && text[space + 1] === "{" ? `${text.slice(0, space)}\n${prettyJSON(text.slice(space + 1))}` : text;
  }
}

function approvalSummary(approval: ChatApproval) {
  let input = approval.input_json;
  try {
    input = JSON.stringify(JSON.parse(approval.input_json));
  } catch {}
  return `${approval.tool_name} ${input.length > 80 ? `${input.slice(0, 80)}…` : input}`;
}

function ApprovalCard({ approval, onAnswer }: { approval: ChatApproval; onAnswer: (allow: boolean) => void }) {
  let input = approval.input_json;
  try {
    input = JSON.stringify(JSON.parse(approval.input_json), null, 2);
  } catch {}
  return (
    <section className="panel approval" aria-label={`Approval request for ${approval.tool_name}`}>
      <p>
        <strong>Allow {approval.tool_name}?</strong>
      </p>
      <pre className="mcp-config">
        <code>{input}</code>
      </pre>
      <div className="connector-buttons">
        <button className="primary" onClick={() => onAnswer(true)}>
          Allow once
        </button>
        <button onClick={() => onAnswer(false)}>Deny</button>
      </div>
    </section>
  );
}

type BrowsedFolder = { name: string; path: string };

function FolderDialog({
  current,
  onPick,
  onClose,
  onSettings,
}: {
  current: string | null;
  onPick: (folder: string | null) => Promise<void>;
  onClose: () => void;
  onSettings: () => void;
}) {
  const [roots, setRoots] = useState<AllowedFolder[]>();
  const [trail, setTrail] = useState<BrowsedFolder[]>([]);
  const [folders, setFolders] = useState<BrowsedFolder[]>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>();
  const dialog = useRef<HTMLDialogElement>(null);
  const at = trail.at(-1);

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
    api.get<AllowedFolder[]>("/api/settings/folders").then(setRoots).catch(setError);
  }, []);

  useEffect(() => {
    if (!at) return;
    let current = true;
    setFolders(undefined);
    api
      .get<{ folders: BrowsedFolder[] }>(`/api/settings/folders/browse?path=${encodeURIComponent(at.path)}`)
      .then((result) => current && setFolders(result.folders))
      .catch((reason) => current && setError(reason));
    return () => {
      current = false;
    };
  }, [at?.path]);

  async function pick(folder: string | null) {
    setSaving(true);
    try {
      await onPick(folder);
    } catch (reason) {
      setError(reason);
      setSaving(false);
    }
  }

  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="folder-dialog-title" onClose={onClose}>
      <section className="panel create create-modal">
        <div className="page-heading">
          <h2 id="folder-dialog-title">Choose folder</h2>
          <IconButton type="button" className="ghost" icon="x" label="Close" onClick={() => dialog.current?.close()} />
        </div>
        <nav className="folder-trail" aria-label="Folder path">
          <button type="button" className="ghost" aria-current={at ? undefined : "location"} onClick={() => setTrail([])}>
            Folders
          </button>
          {trail.map((folder, index) => (
            <span key={folder.path}>
              <span aria-hidden="true">/</span>
              <button
                type="button"
                className="ghost"
                title={folder.path}
                aria-current={index === trail.length - 1 ? "location" : undefined}
                onClick={() => setTrail(trail.slice(0, index + 1))}
              >
                {folder.name}
              </button>
            </span>
          ))}
        </nav>
        <ul className="folder-browser" aria-busy={at ? !folders : !roots}>
          {!at && (
            <li>
              <button type="button" className="ghost" disabled={saving} onClick={() => pick(null)}>
                <Icon name="folder" /> Scratch folder (no files shared)
                {current === null && <span className="muted">current</span>}
              </button>
            </li>
          )}
          {(at ? folders : roots?.map((root) => ({ name: root.path, path: root.path })))?.map((folder) => (
            <li key={folder.path}>
              <button type="button" className="ghost" onClick={() => (setError(undefined), setTrail([...trail, folder]))}>
                <Icon name="folder" /> {folder.name}
                {current === folder.path && <span className="muted">current</span>}
              </button>
            </li>
          ))}
          {at && folders?.length === 0 && <li className="muted">No folders inside.</li>}
        </ul>
        <small className="muted">
          Want more folders?{" "}
          <a
            href="/settings"
            onClick={(event) => {
              event.preventDefault();
              dialog.current?.close();
              onSettings();
            }}
          >
            Add them in Settings
          </a>
        </small>
        <ErrorText error={error} />
        <div className="modal-actions">
          <button type="button" onClick={() => dialog.current?.close()}>
            Cancel
          </button>
          <button
            type="button"
            className="primary"
            disabled={!at || saving || at.path === current}
            onClick={() => at && pick(at.path)}
          >
            Use this folder
          </button>
        </div>
      </section>
    </dialog>
  );
}

function NewChatDialog({
  connectors,
  onClose,
  onCreated,
}: {
  connectors: Connector[];
  onClose: () => void;
  onCreated: (chat: Chat) => void;
}) {
  const defaultModel = (id: string) => connectors.find((c) => c.id === id && c.kind === "local")?.model ?? "";
  const [connectorID, setConnectorID] = useState(connectors[0]?.id ?? "");
  const [model, setModel] = useState(() => defaultModel(connectors[0]?.id ?? ""));
  const { models, loading: modelsLoading } = useModels(connectorID);
  const [folder, setFolder] = useState("");
  const [folders, setFolders] = useState<AllowedFolder[]>([]);
  const [error, setError] = useState<unknown>();
  const dialog = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
    api.get<AllowedFolder[]>("/api/settings/folders").then(setFolders).catch(setError);
    api
      .get<ChatDefaults>("/api/settings/defaults")
      .then((defaults) => {
        if (defaults.connector_id && connectors.some((c) => c.id === defaults.connector_id)) {
          setConnectorID(defaults.connector_id);
          setModel(defaultModel(defaults.connector_id));
        }
        if (defaults.folder) setFolder(defaults.folder);
      })
      .catch(setError);
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const chat = await api.post<Chat>("/api/chats", {
        connector_id: connectorID,
        model,
        folder: folder || null,
      });
      onCreated(chat);
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="new-chat-title" onClose={onClose}>
      <section className="panel create create-modal">
        <div className="page-heading">
          <h2 id="new-chat-title">New chat</h2>
          <IconButton type="button" className="ghost" icon="x" label="Close" onClick={() => dialog.current?.close()} />
        </div>
        <form onSubmit={submit}>
          <label>
            Connector
            <select required value={connectorID} onChange={(event) => {
                setConnectorID(event.target.value);
                setModel(defaultModel(event.target.value));
              }}>
              {connectors.length === 0 && <option value="">No chat connector</option>}
              {connectors.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name} · {c.kind}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span className="label-row">
              Model {modelsLoading && <span className="spinner" role="status" aria-label="Loading models" />}
            </span>
            <select
              value={model}
              disabled={modelsLoading}
              aria-busy={modelsLoading}
              onChange={(event) => setModel(event.target.value)}
            >
              <ModelOptions models={models} current={model} loading={modelsLoading} />
            </select>
          </label>
          <label className="wide">
            Folder
            <select value={folder} onChange={(event) => setFolder(event.target.value)}>
              <option value="">Scratch folder (no files shared)</option>
              {folders.map((f) => (
                <option key={f.id} value={f.path}>
                  {f.path}
                </option>
              ))}
            </select>
            {folders.length === 0 && <small>Add folders in Settings → Allowed folders.</small>}
          </label>
          <div className="modal-actions">
            <button type="button" onClick={() => dialog.current?.close()}>
              Cancel
            </button>
            <button className="primary" disabled={!connectorID}>
              Start chat
            </button>
          </div>
        </form>
        <ErrorText error={error} />
      </section>
    </dialog>
  );
}
