import { useEffect, useRef, useState } from "react";
import AIBadge from "./AIBadge";
import {
  api,
  type BoardData,
  type Comment,
  type History,
  type Ticket,
  type Transition,
  type User,
} from "./api";
import { type Config, ErrorText, formatDate, navigate } from "./App";
import { Icon } from "./icons";
import Markdown from "./Markdown";
import MarkdownEditor from "./MarkdownEditor";
import Picker from "./Picker";

export default function TicketDetail({
  id,
  config,
  refresh,
}: {
  id: string;
  config: Config;
  refresh: number;
}) {
  const [ticket, setTicket] = useState<Ticket>();
  const [board, setBoard] = useState<BoardData>();
  const [history, setHistory] = useState<History[]>([]);
  const [transitions, setTransitions] = useState<Transition[]>([]);
  const [comments, setComments] = useState<Comment[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [error, setError] = useState<unknown>();
  const [form, setForm] = useState({
    title: "",
    description: "",
    type: "",
    dueDate: "",
    status: "",
    assignee: "",
  });
  const [tab, setTab] = useState<"comments" | "history" | "transitions">(
    "comments",
  );
  const previous = useRef({ id, showHidden: config.show_hidden, refresh });
  async function load(preserveForm = false) {
    try {
      const current = await api.get<Ticket>(`/api/tickets/${id}`);
      setTicket(current);
      // Title and description save on blur, so keep in-progress text on live refresh.
      setForm((prev) => ({
        title: preserveForm ? prev.title : current.title,
        description: preserveForm ? prev.description : current.description,
        type: current.ticket_type_id || "",
        dueDate: current.due_date?.slice(0, 10) || "",
        status: current.status_id,
        assignee: current.assignee_id || "",
      }));
      const [boardData, ticketHistory, ticketTransitions] = await Promise.all([
        api.get<BoardData>(`/api/boards/${current.board_id}/data`),
        api.get<History[]>(`/api/tickets/${current.id}/history`),
        api.get<Transition[]>(`/api/tickets/${current.id}/transitions`),
      ]);
      setBoard(boardData);
      setHistory(ticketHistory);
      setTransitions(ticketTransitions);
      if (config.show_hidden) {
        setComments(
          await api.get<Comment[]>(`/api/tickets/${current.id}/comments`),
        );
        setUsers(await api.get<User[]>("/api/users"));
      }
    } catch (reason) {
      setError(reason);
    }
  }
  useEffect(() => {
    const preserveForm =
      previous.current.id === id &&
      previous.current.showHidden === config.show_hidden &&
      previous.current.refresh !== refresh;
    previous.current = { id, showHidden: config.show_hidden, refresh };
    load(preserveForm);
  }, [id, config.show_hidden, refresh]);
  async function save(patch: unknown) {
    if (!ticket) return;
    try {
      await api.put(`/api/tickets/${ticket.id}`, patch);
      await load();
    } catch (reason) {
      setError(reason);
    }
  }
  if (!ticket)
    return (
      <div className="loading">
        Loading ticket...
        <ErrorText error={error} />
      </div>
    );
  const activeTab = !config.show_hidden && tab === "comments" ? "history" : tab;
  const tabs = [
    ...(config.show_hidden ? ([["comments", "Comments"]] as const) : []),
    ["history", "History"],
    ["transitions", "Transitions"],
  ] as const;
  return (
    <div className="ticket-page">
      <button
        className="back"
        onClick={() => navigate(`/boards/${ticket.board_id}`)}
      >
        <Icon name="arrowLeft" /> Back to board
      </button>
      <ErrorText error={error} />
      <section className="panel ticket-shell">
        <div className="ticket-top">
          <span className="ticket-code">{ticket.code}</span>
          <div className="ticket-links">
            <a
              href={`/print/tickets/${ticket.id}`}
              onClick={(event) => {
                event.preventDefault();
                navigate(`/print/tickets/${ticket.id}`);
              }}
            >
              <Icon name="printer" /> Print
            </a>
            <a href={`/api/tickets/${ticket.id}/export?format=json`} download="">
              <Icon name="download" /> Export JSON
            </a>
            <a href={`/api/tickets/${ticket.id}/export?format=md`} download="">
              <Icon name="download" /> Export Markdown
            </a>
          </div>
        </div>
        <div className="ticket-layout">
          <div className="ticket-main">
            <input
              className="title-input"
              aria-label="Title"
              value={form.title}
              onChange={(event) =>
                setForm({ ...form, title: event.target.value })
              }
              onBlur={() => save({ title: form.title })}
            />
            <div className="field">
              Description
              <MarkdownEditor
                label="Description"
                value={form.description}
                onChange={(description) => setForm({ ...form, description })}
                onBlur={() => save({ description: form.description })}
              />
            </div>
          </div>
          <aside className="ticket-side">
            <div className="field">
              Status
              <Picker
                label="Status"
                options={board?.statuses ?? []}
                value={form.status}
                onChange={(status) => {
                  setForm({ ...form, status });
                  save({ status_id: status });
                }}
              />
            </div>
            <div className="ticket-details">
              <div className="field">
                Type
                <Picker
                  label="Type"
                  empty="No type"
                  options={board?.ticket_types ?? []}
                  value={form.type}
                  onChange={(type) => {
                    setForm({ ...form, type });
                    save({ ticket_type_id: type || null });
                  }}
                />
              </div>
              <label>
                Due date
                <input
                  type="date"
                  value={form.dueDate}
                  onChange={(event) => {
                    setForm({ ...form, dueDate: event.target.value });
                    save({ due_date: event.target.value || null });
                  }}
                />
              </label>
              {config.show_hidden && (
                <label>
                  Assignee
                  <Picker
                    label="Assignee"
                    empty="Unassigned"
                    options={users.map((user) => ({
                      id: user.id,
                      name: `${user.name} ${user.last_name}${user.is_ai ? " (AI)" : ""}`,
                    }))}
                    value={form.assignee}
                    onChange={(assignee) => {
                      setForm({ ...form, assignee });
                      save({ assignee_id: assignee || null });
                    }}
                  />
                </label>
              )}
              <dl>
                <dt>Created</dt>
                <dd>{formatDate(ticket.created_at)}</dd>
                <dt>Updated</dt>
                <dd>{formatDate(ticket.updated_at)}</dd>
              </dl>
            </div>
          </aside>
        </div>
        <div className="tabs" role="tablist">
          {tabs.map(([key, label]) => (
            <button
              key={key}
              role="tab"
              id={`tab-${key}`}
              aria-selected={activeTab === key}
              aria-controls="ticket-tabpanel"
              onClick={() => setTab(key)}
            >
              {label}
            </button>
          ))}
        </div>
        <div
          className="tabpanel"
          role="tabpanel"
          id="ticket-tabpanel"
          aria-labelledby={`tab-${activeTab}`}
        >
          {activeTab === "comments" && (
            <Comments ticketId={ticket.id} comments={comments} onReload={load} />
          )}
          {activeTab === "history" &&
            history.map((item) => (
              <div className="timeline" key={item.id}>
                <strong>
                  {item.user_name} <AIBadge isAI={item.user_is_ai} />
                </strong>
                <span>{formatDate(item.created_at)}</span>
                <p>{item.content}</p>
              </div>
            ))}
          {activeTab === "transitions" &&
            transitions.map((item) => (
              <div className="transition" key={item.id}>
                <strong>
                  {item.from_status_name || "New"} → {item.to_status_name}
                </strong>
                <span>
                  {item.user_name} <AIBadge isAI={item.user_is_ai} /> ·{" "}
                  {formatDate(item.moved_at)}
                </span>
              </div>
            ))}
        </div>
      </section>
    </div>
  );
}

function Comments({
  ticketId,
  comments,
  onReload,
}: {
  ticketId: string;
  comments: Comment[];
  onReload: () => void;
}) {
  const [content, setContent] = useState("");
  async function add() {
    if (!content.trim()) return;
    await api.post(`/api/tickets/${ticketId}/comments`, {
      content,
    });
    setContent("");
    onReload();
  }
  return (
    <section className="comments">
      <MarkdownEditor label="Comment" value={content} onChange={setContent} />
      <button className="primary" onClick={add}>
        <Icon name="send" /> Add comment
      </button>
      {comments.map((comment) => (
        <div className="comment" key={comment.id}>
          <strong>
            {comment.user_name} <AIBadge isAI={comment.user_is_ai} />
          </strong>
          <span>{formatDate(comment.created_at)}</span>
          <Markdown text={comment.content} />
        </div>
      ))}
    </section>
  );
}
