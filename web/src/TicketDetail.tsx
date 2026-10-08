import { useEffect, useState } from "react";
import DOMPurify from "dompurify";
import ReactQuill from "react-quill-new";
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

const clean = (html: string) => DOMPurify.sanitize(html || "");

export default function TicketDetail({
  id,
  config,
}: {
  id: string;
  config: Config;
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
  async function load() {
    try {
      const current = await api.get<Ticket>(`/api/tickets/${id}`);
      setTicket(current);
      setForm({
        title: current.title,
        description: clean(current.description),
        type: current.ticket_type_id || "",
        dueDate: current.due_date?.slice(0, 10) || "",
        status: current.status_id,
        assignee: current.assignee_id || "",
      });
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
    load();
  }, [id, config.show_hidden]);
  async function save(patch: unknown) {
    if (!ticket) return;
    try {
      await api.put(`/api/tickets/${ticket.id}`, patch);
      await load();
    } catch (reason) {
      setError(reason);
    }
  }
  async function remove() {
    if (!ticket || !confirm("Delete this ticket?")) return;
    try {
      await api.delete(`/api/tickets/${ticket.id}`);
      navigate(`/boards/${ticket.board_id}`);
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
  return (
    <div className="ticket-page">
      <button
        className="back"
        onClick={() => navigate(`/boards/${ticket.board_id}`)}
      >
        ← Back to board
      </button>
      <ErrorText error={error} />
      <div className="ticket-layout">
        <section className="panel ticket-main">
          <div className="ticket-code">{ticket.code}</div>
          <input
            className="title-input"
            aria-label="Title"
            value={form.title}
            onChange={(event) =>
              setForm({ ...form, title: event.target.value })
            }
            onBlur={() => save({ title: form.title })}
          />
          <label>
            Description
            <ReactQuill
              theme="snow"
              value={form.description}
              onChange={(description) => setForm({ ...form, description })}
              onBlur={() => save({ description: clean(form.description) })}
            />
          </label>
          <h2>History</h2>
          {history.map((item) => (
            <div className="timeline" key={item.id}>
              <strong>{item.user_name}</strong>
              <span>{formatDate(item.created_at)}</span>
              <p>{item.content}</p>
            </div>
          ))}
        </section>
        <aside className="panel ticket-side">
          <label>
            Status
            <select
              value={form.status}
              onChange={(event) => {
                setForm({ ...form, status: event.target.value });
                save({ status_id: event.target.value });
              }}
            >
              {board?.statuses.map((status) => (
                <option key={status.id} value={status.id}>
                  {status.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Type
            <select
              value={form.type}
              onChange={(event) => {
                setForm({ ...form, type: event.target.value });
                save({ ticket_type_id: event.target.value || null });
              }}
            >
              <option value="">No type</option>
              {board?.ticket_types.map((type) => (
                <option key={type.id} value={type.id}>
                  {type.name}
                </option>
              ))}
            </select>
          </label>
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
            <>
              <label>
                Assignee
                <select
                  value={form.assignee}
                  onChange={(event) => {
                    setForm({ ...form, assignee: event.target.value });
                    save({ assignee_id: event.target.value || null });
                  }}
                >
                  <option value="">Unassigned</option>
                  {users.map((user) => (
                    <option key={user.id} value={user.id}>
                      {user.name} {user.last_name}
                    </option>
                  ))}
                </select>
              </label>
              <Comments
                ticketId={ticket.id}
                comments={comments}
                onReload={load}
              />
            </>
          )}
          <button className="danger" onClick={remove}>
            Delete ticket
          </button>
          <h2>Transitions</h2>
          {transitions.map((item) => (
            <div className="transition" key={item.id}>
              <strong>
                {item.from_status_name || "New"} → {item.to_status_name}
              </strong>
              <span>
                {item.user_name} · {formatDate(item.moved_at)}
              </span>
            </div>
          ))}
        </aside>
      </div>
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
      content: clean(content),
    });
    setContent("");
    onReload();
  }
  return (
    <section className="comments">
      <h2>Comments</h2>
      <ReactQuill theme="snow" value={content} onChange={setContent} />
      <button onClick={add}>Add comment</button>
      {comments.map((comment) => (
        <div className="comment" key={comment.id}>
          <strong>{comment.user_name}</strong>
          <span>{formatDate(comment.created_at)}</span>
          <div dangerouslySetInnerHTML={{ __html: clean(comment.content) }} />
        </div>
      ))}
    </section>
  );
}
