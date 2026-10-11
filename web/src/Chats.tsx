import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import Markdown from "./Markdown";
import {
  api,
  type AllowedFolder,
  type Chat,
  type ChatApproval,
  type ChatMessage,
  type Connector,
} from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";
import Switch from "./Switch";

type ChatDetail = { chat: Chat; messages: ChatMessage[]; pending_approvals: ChatApproval[] };
type ChatEvent =
  | { type: "delta"; text: string }
  | { type: "tool"; message: ChatMessage }
  | { type: "done"; message?: ChatMessage }
  | { type: "error"; message: string }
  | { type: "approval" };

type ModelOption = { id: string; label: string; description?: string };

const fallbackModels: ModelOption[] = [{ id: "", label: "Default" }];
const modelCache = new Map<string, ModelOption[]>();

function useModels(connectorID: string | null | undefined) {
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

function ModelOptions({ models, current, loading }: { models: ModelOption[]; current: string; loading: boolean }) {
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

export default function Chats({ id, refresh }: { id?: string; refresh: number }) {
  const [chats, setChats] = useState<Chat[]>([]);
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);

  useEffect(() => {
    Promise.all([api.get<Chat[]>("/api/chats"), api.get<Connector[]>("/api/connectors")])
      .then(([chatList, connectorList]) => {
        setChats(chatList);
        setConnectors(connectorList);
        setError(undefined);
      })
      .catch(setError);
  }, [refresh, reload, id]);

  return (
    <div className="chats">
      <aside className="chat-list" aria-label="Chats">
        <button className="primary" onClick={() => setCreating(true)}>
          <Icon name="plus" /> New chat
        </button>
        <ErrorText error={error} />
        {chats.length === 0 && !error && <p className="muted">No chats yet.</p>}
        <nav>
          {chats.map((chat) => (
            <button
              key={chat.id}
              className="chat-link"
              aria-current={chat.id === id ? "page" : undefined}
              onClick={() => navigate(`/ai/chats/${chat.id}`)}
            >
              <span>{chat.title}</span>
              <small>{new Date(chat.updated_at).toLocaleDateString()}</small>
            </button>
          ))}
        </nav>
      </aside>
      {id ? (
        <Conversation
          key={id}
          id={id}
          refresh={refresh}
          connectors={connectors}
          onChanged={() => setReload((value) => value + 1)}
        />
      ) : (
        <section className="chat-empty">
          <h1>Chats</h1>
          <p className="muted">
            {connectors.length
              ? "Pick a chat or start a new one."
              : "Add a connector first (Claude Code, Codex, Copilot, opencode, or Local AI), then start a chat."}
          </p>
          {!connectors.length && (
            <button onClick={() => navigate("/ai/connectors")}>Go to Connectors</button>
          )}
        </section>
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

function Conversation({
  id,
  refresh,
  connectors,
  onChanged,
}: {
  id: string;
  refresh: number;
  connectors: Connector[];
  onChanged: () => void;
}) {
  const [detail, setDetail] = useState<ChatDetail>();
  const [streaming, setStreaming] = useState("");
  const [running, setRunning] = useState(false);
  const [text, setText] = useState("");
  const [title, setTitle] = useState("");
  const [error, setError] = useState<unknown>();
  const [missing, setMissing] = useState(false);
  const [announcement, setAnnouncement] = useState("");
  const end = useRef<HTMLDivElement>(null);
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
    const unsubscribe = window.runtime?.EventsOn(`chat:${id}`, (data) => {
      const event = data as ChatEvent;
      if (event.type === "approval") void load();
      if (event.type === "delta") setStreaming((current) => current + event.text);
      if (event.type === "tool" || event.type === "done") {
        if (event.type === "done") {
          setStreaming("");
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
        setRunning(false);
        setError(event.message);
      }
    });
    return () => unsubscribe?.();
  }, [id]);

  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [detail?.messages.length, streaming, detail?.pending_approvals.length]);

  async function send(event?: FormEvent) {
    event?.preventDefault();
    const value = text.trim();
    if (!value || running || !detail) return;
    setError(undefined);
    setRunning(true);
    try {
      const message = await api.post<ChatMessage>(`/api/chats/${id}/messages`, { text: value });
      setText("");
      setDetail((current) =>
        current && !current.messages.some((m) => m.id === message.id)
          ? { ...current, messages: [...current.messages, message] }
          : current,
      );
      if (detail.chat.title === "New chat" && detail.messages.length === 0) {
        await rename(value.length > 48 ? `${value.slice(0, 48)}…` : value);
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

  async function update(fields: { model: string } | { thinking: boolean } | { tools: boolean }) {
    try {
      await api.put(`/api/chats/${id}`, fields);
      setDetail((current) => current && { ...current, chat: { ...current.chat, ...fields } });
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
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
    if (!confirm(`Delete chat "${detail?.chat.title}"?`)) return;
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

  return (
    <section className="conversation" aria-labelledby="chat-title">
      <header className="conversation-head">
        <div className="conversation-title">
          <label className="sr-only" htmlFor="chat-title">
            Chat title
          </label>
          <input
            id="chat-title"
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            onBlur={(event) => rename(event.target.value)}
            onKeyDown={(event) => event.key === "Enter" && event.currentTarget.blur()}
          />
          <div className="conversation-meta">
            <span>{connector ? connector.name : "Connector removed"}</span>
            <label>
              <span className="sr-only">Model</span>
              <select
                value={detail.chat.model}
                disabled={!connector || running || modelsLoading}
                aria-busy={modelsLoading}
                title={running ? "Stop the reply to change the model" : "Model"}
                onChange={(event) => update({ model: event.target.value })}
              >
                <ModelOptions models={models} current={detail.chat.model} loading={modelsLoading} />
              </select>
              {modelsLoading && <span className="sr-only" role="status">Loading models</span>}
            </label>
            {connector?.kind === "local" && (
              <Switch
                label="Tools"
                checked={detail.chat.tools}
                disabled={running}
                title={
                  running
                    ? "Stop the reply to change tools"
                    : "Let the model use files in this folder and your tickets. Turn off for plain conversation."
                }
                onChange={(tools) => update({ tools })}
              />
            )}
            {connector?.kind === "local" && (
              <Switch
                label="Thinking"
                checked={detail.chat.thinking}
                disabled={running}
                title={
                  running
                    ? "Stop the reply to change thinking"
                    : "Let models that think reason before answering. Slower; no effect on other models."
                }
                onChange={(thinking) => update({ thinking })}
              />
            )}
            <button
              type="button"
              className="folder-chip"
              title={`Open in file manager: ${detail.chat.workdir}`}
              onClick={() => reveal(detail.chat.workdir)}
            >
              <Icon name="folder" /> {detail.chat.folder ?? "Scratch folder (no files shared)"}
            </button>
          </div>
        </div>
        <IconButton className="danger-text" icon="trash" label="Delete chat" onClick={remove} />
      </header>

      <p className="sr-only" role="status">
        {running ? "Assistant is replying." : announcement}
      </p>
      <div className="messages">
        {detail.messages.length === 0 && !streaming && (
          <p className="muted chat-hint">
            Ask anything. The assistant can read files in this chat's folder, use your tickets, and asks before
            changing anything.
          </p>
        )}
        {detail.messages.map((message) =>
          message.role === "tool" ? (
            <p key={message.id} className="tool-line">
              <Icon name="plug" /> <code>{message.content}</code> <MessageTime iso={message.created_at} />
            </p>
          ) : (
            <div key={message.id} className={`message-row ${message.role}`}>
              <div className={`message ${message.role}`}>
                {message.role === "assistant" ? <Markdown text={message.content} onFile={reveal} /> : message.content}
              </div>
              <MessageTime iso={message.created_at} />
            </div>
          ),
        )}
        {streaming && (
          <div className="message assistant">
            <Markdown text={streaming} onFile={reveal} />
          </div>
        )}
        {running && !streaming && <p className="muted typing">Thinking…</p>}
        {detail.pending_approvals.map((approval) => (
          <ApprovalCard key={approval.id} approval={approval} onAnswer={(allow) => answer(approval, allow)} />
        ))}
        <div ref={end} />
      </div>

      <ErrorText error={error} />
      <form className="composer" onSubmit={send}>
        <label className="sr-only" htmlFor="chat-input">
          Message
        </label>
        <textarea
          id="chat-input"
          rows={3}
          placeholder={connector ? "Message… (Enter to send, Shift+Enter for a new line)" : "This chat's connector was removed"}
          disabled={!connector}
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={onKeyDown}
        />
        {running ? (
          <button type="button" onClick={stop}>
            Stop
          </button>
        ) : (
          <button className="primary" disabled={!text.trim() || !connector}>
            <Icon name="send" /> Send
          </button>
        )}
      </form>
    </section>
  );
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
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const chat = await api.post<Chat>("/api/chats", {
        connector_id: connectorID,
        model,
        ...(folder ? { folder } : {}),
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
