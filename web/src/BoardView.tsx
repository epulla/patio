import {
  useEffect,
  useRef,
  useState,
  type DragEvent,
  type KeyboardEvent,
} from "react";
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
          <ExportMenu boardId={id} />
          <button onClick={() => setSettings(!settings)}>
            <Icon name={settings ? "x" : "settings"} />
            {settings ? "Close settings" : "Board settings"}
          </button>
        </div>
      </div>
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

const exportFormats = [
  ["xlsx", "Excel (.xlsx)", "Spreadsheet of tickets"],
  ["csv", "CSV", "Plain table"],
  ["json", "JSON backup", "Import into my-tickets on another computer"],
];

function ExportMenu({ boardId }: { boardId: string }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const items = () => [
    ...(root.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? []),
  ];
  useEffect(() => {
    if (!open) return;
    items()[0]?.focus();
    const outside = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", outside);
    return () => document.removeEventListener("mousedown", outside);
  }, [open]);
  function close() {
    setOpen(false);
    trigger.current?.focus();
  }
  function keyDown(event: KeyboardEvent) {
    if (!open) return;
    if (event.key === "Escape") return close();
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    const list = items();
    const step = event.key === "ArrowDown" ? 1 : -1;
    const index = list.indexOf(document.activeElement as HTMLElement);
    list[(index + step + list.length) % list.length].focus();
  }
  return (
    <div
      className="menu"
      ref={root}
      onKeyDown={keyDown}
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <button
        ref={trigger}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <Icon name="download" /> Export <Icon name="chevronDown" />
      </button>
      {open && (
        <div className="menu-list" role="menu" aria-label="Export" onMouseDown={(event) => event.preventDefault()}>
          {exportFormats.map(([format, label, hint]) => (
            <a
              key={format}
              role="menuitem"
              href={`/api/boards/${boardId}/export?format=${format}`}
              download=""
              // Defer so the browser starts the download before the item unmounts.
              onClick={() => setTimeout(close)}
            >
              <strong>{label}</strong>
              <small>{hint}</small>
            </a>
          ))}
        </div>
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
  const [newColumn, setNewColumn] = useState<string>();
  async function addColumn(event: React.FormEvent) {
    event.preventDefault();
    if (!newColumn?.trim()) return;
    try {
      await api.post(`/api/boards/${data.board.id}/statuses`, {
        name: newColumn.trim(),
        type: "classic",
      });
      setNewColumn(undefined);
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
      {newColumn === undefined ? (
        <IconButton
          className="add-column"
          icon="plus"
          label="Add column"
          onClick={() => setNewColumn("")}
        />
      ) : (
        <form
          className="column new-column"
          onSubmit={addColumn}
          onKeyDown={(event) => event.key === "Escape" && setNewColumn(undefined)}
        >
          <input
            autoFocus
            required
            aria-label="Column name"
            placeholder="Column name"
            value={newColumn}
            onChange={(event) => setNewColumn(event.target.value)}
          />
          <IconButton className="primary" icon="check" label="Create column" />
          <IconButton
            type="button"
            className="ghost"
            icon="x"
            label="Cancel"
            onClick={() => setNewColumn(undefined)}
          />
        </form>
      )}
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
  const tempTip = `Temporary column: tickets hide ${status.hide_after_days} day${status.hide_after_days === 1 ? "" : "s"} after entering it. They are not deleted.`;
  return (
    <section
      className={`column ${status.type === "temporary" ? "temporary" : ""} ${over ? "drop" : ""}`}
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
            <span
              className="help-tip tip-wide"
              role="img"
              tabIndex={0}
              aria-label={tempTip}
              data-tip={tempTip}
            >
              ?
            </span>
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
  const open = () => navigate(`/tickets/${ticket.code || ticket.id}`);
  return (
    <article
      className="ticket-card"
      data-ticket={ticket.id}
      draggable
      onDragStart={(event) => onDrag(event, "ticket", ticket.id)}
      role="link"
      tabIndex={0}
      onClick={open}
      onKeyDown={(event) => event.key === "Enter" && open()}
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
          <IconButton icon="check" label="Save board" onClick={saveBoard} />
          <IconButton
            className="danger-text"
            icon="trash"
            label="Delete board"
            onClick={deleteBoard}
          />
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
  // Bumped after each move so rows remount and drop unsaved edits.
  const [moves, setMoves] = useState(0);
  async function move(index: number, to: number) {
    const ids = statuses.map((status) => status.id);
    [ids[index], ids[to]] = [ids[to], ids[index]];
    try {
      await api.put(`/api/boards/${statuses[index].board_id}/status-order`, {
        status_ids: ids,
      });
      setMoves((count) => count + 1);
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  return (
    <div className="settings-section">
      <h2>Columns</h2>
      <small>
        Temporary: tickets auto-hide from the board N days after entering the column. They are not deleted.
      </small>
      {statuses.map((status) => (
        <StatusRow
          key={`${moves}-${status.id}`}
          status={status}
          onReload={onReload}
          onError={onError}
        />
      ))}
      <h3 className="preview-title">Order</h3>
      <ol className="column-preview" aria-label="Column order">
        {statuses.map((status, index) => (
          <li
            key={status.id}
            className={`preview-card ${status.type === "temporary" ? "temporary" : ""}`}
          >
            <span>{status.name}</span>
            <div>
              <IconButton
                className="ghost"
                icon="chevronLeft"
                label={`Move ${status.name} left`}
                disabled={index === 0}
                onClick={() => move(index, index - 1)}
              />
              <IconButton
                className="ghost"
                icon="chevronRight"
                label={`Move ${status.name} right`}
                disabled={index === statuses.length - 1}
                onClick={() => move(index, index + 1)}
              />
            </div>
          </li>
        ))}
      </ol>
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
  const [hideAfterDays, setHideAfterDays] = useState(
    String(status.hide_after_days),
  );
  async function save() {
    try {
      await api.put(`/api/statuses/${status.id}`, {
        name,
        type,
        hide_after_days: Number(hideAfterDays),
      });
      onReload();
    } catch (error) {
      onError(error);
    }
  }
  async function remove() {
    if (!confirm(`Delete column "${status.name}"?`)) return;
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
      <DaysField
        label={`${status.name} hide after days`}
        value={hideAfterDays}
        onChange={setHideAfterDays}
        hidden={type !== "temporary"}
      />
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

// Stays in layout when hidden so toggling Classic/Temporary doesn't shift the row.
export function DaysField({
  label,
  value,
  onChange,
  hidden,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  hidden: boolean;
}) {
  return (
    <label
      className={`days-field ${hidden ? "is-hidden" : ""}`}
      data-tip="Hide tickets after N days"
    >
      <input
        type="number"
        min={1}
        step={1}
        required
        disabled={hidden}
        aria-label={label}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      <span>days</span>
    </label>
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
    if (!confirm(`Delete ticket type "${type.name}"?`)) return;
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
    <div
      className="modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="new-ticket-title"
      onKeyDown={(event) => event.key === "Escape" && onDone()}
    >
      <form className="panel ticket-form" onSubmit={submit}>
        <div className="page-heading">
          <h2 id="new-ticket-title">New ticket</h2>
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
            autoFocus
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
