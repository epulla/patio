import { useEffect, useState, type DragEvent } from "react";
import ReactQuill from "react-quill-new";
import {
  api,
  type BoardData,
  type Status,
  type Ticket,
  type TicketType,
} from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";

export default function BoardView({
  id,
  refresh,
}: {
  id: string;
  refresh: number;
}) {
  const [data, setData] = useState<BoardData>();
  const [error, setError] = useState<unknown>();
  const [query, setQuery] = useState(
    new URLSearchParams(location.search).get("q") || "",
  );
  const [settings, setSettings] = useState(false);
  const [newTicketStatus, setNewTicketStatus] = useState<string>();

  const load = () =>
    api
      .get<BoardData>(`/api/boards/${id}/data?q=${encodeURIComponent(query)}`)
      .then(setData)
      .catch(setError);
  useEffect(() => {
    load();
  }, [id, query, refresh]);

  function search(value: string) {
    setQuery(value);
    history.replaceState(
      {},
      "",
      `/boards/${id}${value ? `?q=${encodeURIComponent(value)}` : ""}`,
    );
  }

  if (!data)
    return (
      <div className="loading">
        Loading board...
        <ErrorText error={error} />
      </div>
    );
  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <button className="back" onClick={() => navigate("/")}>
            <Icon name="arrowLeft" /> Boards
          </button>
          <h1>{data.board.name}</h1>
        </div>
        <div className="actions">
          <input
            className="search"
            aria-label="Search tickets"
            placeholder="Search tickets..."
            value={query}
            onChange={(event) => search(event.target.value)}
          />
          <button onClick={() => setSettings(!settings)}>
            <Icon name={settings ? "x" : "settings"} />
            {settings ? "Close settings" : "Board settings"}
          </button>
        </div>
      </div>
      <section className="export-bar">
        <strong>Export</strong>
        <div className="export-links">
          <a href={`/api/boards/${id}/export?format=xlsx`} download="">
            <Icon name="download" /> Excel (.xlsx)
          </a>
          <a href={`/api/boards/${id}/export?format=csv`} download="">
            <Icon name="download" /> CSV
          </a>
          <a href={`/api/boards/${id}/export?format=json`} download="" aria-describedby="board-export-hint">
            <Icon name="download" /> JSON backup
          </a>
        </div>
        <small id="board-export-hint">Full board backup — import into my-tickets on another computer.</small>
      </section>
      <ErrorText error={error} />
      {settings ? (
        <Settings data={data} onReload={load} onError={setError} />
      ) : (
        <Kanban
          data={data}
          onReload={load}
          onError={setError}
          onAdd={(status) => setNewTicketStatus(status)}
        />
      )}
      {newTicketStatus && (
        <NewTicket
          data={data}
          statusId={newTicketStatus}
          onDone={() => {
            setNewTicketStatus(undefined);
            load();
          }}
          onError={setError}
        />
      )}
    </div>
  );
}

function Kanban({
  data,
  onReload,
  onError,
  onAdd,
}: {
  data: BoardData;
  onReload: () => void;
  onError: (error: unknown) => void;
  onAdd: (statusId: string) => void;
}) {
  async function addColumn() {
    const name = prompt("Column name");
    if (!name) return;
    try {
      await api.post(`/api/boards/${data.board.id}/statuses`, {
        name,
        type: "classic",
      });
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  return (
    <div className="kanban">
      {data.statuses.map((status, index) => (
        <Column
          key={status.id}
          status={status}
          index={index}
          statuses={data.statuses}
          types={data.ticket_types}
          onReload={onReload}
          onError={onError}
          onAdd={onAdd}
        />
      ))}
      <IconButton className="add-column" icon="plus" label="Add column" onClick={addColumn} />
    </div>
  );
}

function Column({
  status,
  index,
  statuses,
  types,
  onReload,
  onError,
  onAdd,
}: {
  status: Status;
  index: number;
  statuses: Status[];
  types: TicketType[];
  onReload: () => void;
  onError: (error: unknown) => void;
  onAdd: (statusId: string) => void;
}) {
  const [over, setOver] = useState(false);
  const dragStart = (event: DragEvent, kind: string, id: string) =>
    event.dataTransfer.setData(kind, id);
  async function moveColumn(targetIndex: number) {
    const ids = statuses.map((item) => item.id);
    const [moved] = ids.splice(index, 1);
    ids.splice(targetIndex, 0, moved);
    try {
      await api.put(`/api/boards/${status.board_id}/status-order`, {
        status_ids: ids,
      });
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  async function drop(event: DragEvent) {
    event.preventDefault();
    setOver(false);
    const ticketId = event.dataTransfer.getData("ticket");
    const columnId = event.dataTransfer.getData("column");
    try {
      if (ticketId) {
        const target =
          event.target instanceof HTMLElement
            ? event.target.closest("[data-ticket]")
            : null;
        const tickets = status.tickets.filter(
          (ticket) => ticket.id !== ticketId,
        );
        const targetId = target?.getAttribute("data-ticket");
        const targetIndex = targetId
          ? Math.max(
              0,
              tickets.findIndex((ticket) => ticket.id === targetId),
            )
          : tickets.length;
        await api.put(`/api/tickets/${ticketId}/move`, {
          status_id: status.id,
          index: targetIndex,
        });
        onReload();
      } else if (columnId && columnId !== status.id) {
        const source = statuses.findIndex((item) => item.id === columnId);
        const ids = statuses.map((item) => item.id);
        const [moved] = ids.splice(source, 1);
        ids.splice(index, 0, moved);
        await api.put(`/api/boards/${status.board_id}/status-order`, {
          status_ids: ids,
        });
        onReload();
      }
    } catch (error) {
      onError(error);
    }
  }
  return (
    <section
      className={`column ${over ? "drop" : ""}`}
      onDragOver={(event) => {
        event.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={drop}
    >
      <div
        className="column-head"
        draggable
        onDragStart={(event) => dragStart(event, "column", status.id)}
      >
        <div>
          <h3>{status.name}</h3>
          <span className="count">{status.tickets.length}</span>
          {status.type === "temporary" && (
            <span className="badge warning">Temporary</span>
          )}
        </div>
        <div className="column-actions">
          <IconButton
            className="ghost"
            icon="chevronLeft"
            label={`Move ${status.name} left`}
            disabled={index === 0}
            onClick={() => moveColumn(index - 1)}
          />
          <IconButton
            className="ghost"
            icon="chevronRight"
            label={`Move ${status.name} right`}
            disabled={index === statuses.length - 1}
            onClick={() => moveColumn(index + 1)}
          />
        </div>
      </div>
      <div className="cards">
        {status.tickets.map((ticket) => (
          <Card
            key={ticket.id}
            ticket={ticket}
            types={types}
            onDrag={dragStart}
          />
        ))}
      </div>
      <IconButton
        className="add-ticket tip-up"
        icon="plus"
        label={`Add ticket to ${status.name}`}
        onClick={() => onAdd(status.id)}
      />
    </section>
  );
}

function Card({
  ticket,
  types,
  onDrag,
}: {
  ticket: Ticket;
  types: TicketType[];
  onDrag: (event: DragEvent, kind: string, id: string) => void;
}) {
  const type = types.find((item) => item.id === ticket.ticket_type_id);
  return (
    <article
      className="ticket-card"
      data-ticket={ticket.id}
      draggable
      onDragStart={(event) => onDrag(event, "ticket", ticket.id)}
      onClick={() => navigate(`/tickets/${ticket.code || ticket.id}`)}
    >
      <h4>{ticket.title}</h4>
      {ticket.due_date && (
        <small className="due">Due {ticket.due_date.slice(0, 10)}</small>
      )}
      <div className="ticket-meta">
        {type ? (
          <span
            className="type"
            style={{ "--type-color": type.color } as React.CSSProperties}
          >
            {type.name}
          </span>
        ) : (
          <span />
        )}
        <span className="ticket-code">{ticket.code}</span>
      </div>
    </article>
  );
}

function Settings({
  data,
  onReload,
  onError,
}: {
  data: BoardData;
  onReload: () => void;
  onError: (error: unknown) => void;
}) {
  const [name, setName] = useState(data.board.name);
  const [shortName, setShortName] = useState(data.board.short_name);
  async function saveBoard() {
    try {
      await api.put(`/api/boards/${data.board.id}`, {
        name,
        short_name: shortName,
        template: data.board.template,
      });
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  async function deleteBoard() {
    if (!confirm("Delete this board?")) return;
    try {
      await api.delete(`/api/boards/${data.board.id}`);
      navigate("/");
    } catch (error) {
      onError(error);
    }
  }
  return (
    <section className="settings panel">
      <div className="settings-section">
        <h2>Board details</h2>
        <div className="inline-form">
          <label>
            Name
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <label>
            Short name
            <input
              value={shortName}
              onChange={(event) => setShortName(event.target.value)}
            />
          </label>
          <button className="primary" onClick={saveBoard}>
            <Icon name="check" /> Save
          </button>
          <button className="danger" onClick={deleteBoard}>
            <Icon name="trash" /> Delete board
          </button>
        </div>
      </div>
      <StatusManager
        statuses={data.statuses}
        onReload={onReload}
        onError={onError}
      />
      <TypeManager
        boardId={data.board.id}
        types={data.ticket_types}
        onReload={onReload}
        onError={onError}
      />
    </section>
  );
}

function StatusManager({
  statuses,
  onReload,
  onError,
}: {
  statuses: Status[];
  onReload: () => void;
  onError: (error: unknown) => void;
}) {
  return (
    <div className="settings-section">
      <h2>Columns</h2>
      {statuses.map((status) => (
        <StatusRow
          key={status.id}
          status={status}
          onReload={onReload}
          onError={onError}
        />
      ))}
    </div>
  );
}
function StatusRow({
  status,
  onReload,
  onError,
}: {
  status: Status;
  onReload: () => void;
  onError: (error: unknown) => void;
}) {
  const [name, setName] = useState(status.name);
  const [type, setType] = useState(status.type);
  async function save() {
    try {
      await api.put(`/api/statuses/${status.id}`, { name, type });
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  async function remove() {
    try {
      await api.delete(`/api/statuses/${status.id}`);
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  return (
    <div className="type-row">
      <input
        aria-label={`${status.name} name`}
        value={name}
        onChange={(event) => setName(event.target.value)}
      />
      <select
        aria-label={`${status.name} type`}
        value={type}
        onChange={(event) => setType(event.target.value as Status["type"])}
      >
        <option value="classic">Classic</option>
        <option value="temporary">Temporary</option>
      </select>
      <IconButton icon="check" label={`Save ${status.name}`} onClick={save} />
      <IconButton
        className="danger-text"
        icon="trash"
        label={`Delete ${status.name}`}
        onClick={remove}
      />
    </div>
  );
}

function TypeManager({
  boardId,
  types,
  onReload,
  onError,
}: {
  boardId: string;
  types: TicketType[];
  onReload: () => void;
  onError: (error: unknown) => void;
}) {
  const [name, setName] = useState("");
  const [color, setColor] = useState("#7c3aed");
  async function add() {
    if (!name.trim()) return;
    try {
      await api.post(`/api/boards/${boardId}/ticket-types`, { name, color });
      setName("");
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  return (
    <div className="settings-section">
      <h2>Ticket types</h2>
      {types.map((ticketType) => (
        <TypeRow
          key={ticketType.id}
          type={ticketType}
          onReload={onReload}
          onError={onError}
        />
      ))}
      <div className="inline-form">
        <label>
          New type
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label>
          Color
          <input
            type="color"
            value={color}
            onChange={(event) => setColor(event.target.value)}
          />
        </label>
        <button onClick={add}>
          <Icon name="plus" /> Add type
        </button>
      </div>
    </div>
  );
}
function TypeRow({
  type,
  onReload,
  onError,
}: {
  type: TicketType;
  onReload: () => void;
  onError: (error: unknown) => void;
}) {
  const [name, setName] = useState(type.name);
  const [color, setColor] = useState(type.color);
  async function save() {
    try {
      await api.put(`/api/ticket-types/${type.id}`, { name, color });
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  async function remove() {
    try {
      await api.delete(`/api/ticket-types/${type.id}`);
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  return (
    <div className="type-row">
      <input
        aria-label={`${type.name} name`}
        value={name}
        onChange={(event) => setName(event.target.value)}
      />
      <input
        aria-label={`${type.name} color`}
        type="color"
        value={color}
        onChange={(event) => setColor(event.target.value)}
      />
      <IconButton icon="check" label={`Save ${type.name}`} onClick={save} />
      <IconButton
        className="danger-text"
        icon="trash"
        label={`Delete ${type.name}`}
        onClick={remove}
      />
    </div>
  );
}

function NewTicket({
  data,
  statusId,
  onDone,
  onError,
}: {
  data: BoardData;
  statusId: string;
  onDone: () => void;
  onError: (error: unknown) => void;
}) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [type, setType] = useState("");
  const [dueDate, setDueDate] = useState("");
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    try {
      await api.post(`/api/boards/${data.board.id}/tickets`, {
        title,
        description,
        status_id: statusId,
        ticket_type_id: type || null,
        due_date: dueDate || null,
      });
      onDone();
    } catch (error) {
      onError(error);
    }
  }
  return (
    <div className="modal">
      <form className="panel ticket-form" onSubmit={submit}>
        <div className="page-heading">
          <h2>New ticket</h2>
          <IconButton
            type="button"
            className="ghost"
            icon="x"
            label="Close"
            onClick={onDone}
          />
        </div>
        <label>
          Title
          <input
            required
            value={title}
            onChange={(event) => setTitle(event.target.value)}
          />
        </label>
        <label>
          Description
          <ReactQuill
            theme="snow"
            value={description}
            onChange={setDescription}
          />
        </label>
        <div className="two">
          <label>
            Type
            <select
              value={type}
              onChange={(event) => setType(event.target.value)}
            >
              <option value="">No type</option>
              {data.ticket_types.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Due date
            <input
              type="date"
              value={dueDate}
              onChange={(event) => setDueDate(event.target.value)}
            />
          </label>
        </div>
        <button className="primary">
          <Icon name="plus" /> Create ticket
        </button>
      </form>
    </div>
  );
}
