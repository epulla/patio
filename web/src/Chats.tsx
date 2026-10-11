import {
  useEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type DragEvent,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import Markdown from "./Markdown";
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
import Switch from "./Switch";
import { formatBytes } from "./Settings";

type ChatDetail = {
  chat: Chat;
  messages: ChatMessage[];
  pending_approvals: ChatApproval[];
  auto_approvals?: ChatApproval[];
};
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

  async function update(fields: { model: string } | { thinking: boolean } | { auto_allow: boolean }) {
    try {
      await api.put(`/api/chats/${id}`, fields);
      setDetail((current) => current && { ...current, chat: { ...current.chat, ...fields } });
      setError(undefined);
    } catch (reason) {
      setError(reason);
    }
  }

  async function changeAutoAllow(on: boolean) {
    if (on && !confirm("Allow every file change and command in this chat without asking? You can turn it off anytime.")) return;
    await update({ auto_allow: on });
    if (on) void load();
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
    <section
      className={dragging ? "conversation dragging" : "conversation"}
      aria-labelledby="chat-title"
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
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
            <Switch
              label="Thinking"
              checked={detail.chat.capabilities.thinking.supported && detail.chat.thinking}
              disabled={running || !detail.chat.capabilities.thinking.supported}
              title={thinkingTitle(detail.chat, running)}
              onChange={(thinking) => update({ thinking })}
            />
            <button
              type="button"
              className="folder-chip"
              title={`Open in file manager: ${detail.chat.workdir}`}
              onClick={() => reveal(detail.chat.workdir)}
            >
              <Icon name="folder" /> {detail.chat.folder ?? "Scratch folder (no files shared)"}
            </button>
            <Switch
              className="warning"
              label="Auto-allow"
              checked={detail.chat.auto_allow}
              title={
                detail.chat.auto_allow
                  ? "Every file change and command runs without asking"
                  : "Ask before every file change and command"
              }
              onChange={changeAutoAllow}
            />
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
            Ask anything. The assistant can read files in this chat's folder and use your tickets. Attach text files
            and images with the paperclip, drag and drop, or paste.
          </p>
        )}
        {timeline(detail).map((message) =>
          "tool_name" in message ? (
            <p key={message.id} className="tool-line">
              <Icon name="check" /> <code>Auto-allowed {approvalSummary(message)}</code>{" "}
              <MessageTime iso={message.created_at} />
            </p>
          ) : message.role === "tool" ? (
            <p key={message.id} className="tool-line">
              <Icon name="plug" /> <code>{message.content}</code> <MessageTime iso={message.created_at} />
            </p>
          ) : (
            <div key={message.id} className={`message-row ${message.role}`}>
              {(message.role !== "user" || message.content) && (
                <div className={`message ${message.role}`}>
                  {message.role === "assistant" ? <Markdown text={message.content} onFile={reveal} /> : message.content}
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
                            src={`/api/chats/${id}/attachments/${encodeURIComponent(attachment.name)}`}
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
        {streaming && (
          <div className="message assistant">
            <Markdown text={streaming} onFile={reveal} />
          </div>
        )}
        {running && !streaming && <p className="muted typing">Working…</p>}
        {detail.pending_approvals.map((approval) => (
          <ApprovalCard key={approval.id} approval={approval} onAnswer={(allow) => answer(approval, allow)} />
        ))}
        <div ref={end} />
      </div>

      <ErrorText error={error} />
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
        <IconButton
          type="button"
          icon="paperclip"
          label="Attach files"
          disabled={!connector || uploading || pending.length >= maxAttachments}
          onClick={() => filePicker.current?.click()}
        />
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
          onPaste={onPaste}
          onDragOver={onDragOver}
          onDrop={onDrop}
        />
        {running ? (
          <button type="button" onClick={stop}>
            Stop
          </button>
        ) : (
          <button className="primary" disabled={(!text.trim() && !pending.length) || uploading || !connector}>
            <Icon name="send" /> Send
          </button>
        )}
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

function thinkingTitle(chat: Chat, running: boolean) {
  const { supported, reason } = chat.capabilities.thinking;
  if (!supported) return reason;
  if (running) return "Stop the reply to change thinking";
  return ["Let the model reason before answering. Slower but smarter.", reason].filter(Boolean).join(" ");
}

function timeline(detail: ChatDetail): (ChatMessage | ChatApproval)[] {
  const auto = detail.auto_approvals ?? [];
  if (!auto.length) return detail.messages;
  return [...detail.messages, ...auto].sort((a, b) => a.created_at.localeCompare(b.created_at));
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
