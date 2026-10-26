import { confirmDialog } from "./confirm";
import {
  useEffect,
  useRef,
  useState,
  type DragEvent,
  type KeyboardEvent,
} from "react";
import DueBadge, { DueDateInput } from "./DueBadge";
import MarkdownEditor from "./MarkdownEditor";
import Picker from "./Picker";
import {
  api,
  type BoardData,
  type SearchTicket,
  type Status,
  type Ticket,
  type TicketType,
} from "./api";
import { AskAI, CreateTabs, useAiCreate, useCreated } from "./AiCreate";
import { ErrorText, navigate } from "./App";
import { linkText } from "./ChatDock";
import { Icon, IconButton } from "./icons";
import { lang, useText } from "./i18n";

const text = {
  en: {
    loading: "Loading board...", boards: "Boards", search: "Search tickets", searchPlaceholder: "Search tickets...",
    newTicket: "New ticket", closeSettings: "Close settings", boardSettings: "Board settings", excel: "Excel (.xlsx)",
    spreadsheet: "Spreadsheet of tickets", csv: "CSV", plainTable: "Plain table", jsonBackup: "JSON backup",
    importPatio: "Import into Patio on another computer", export: "Export", addColumn: "Add column", columnName: "Column name",
    createColumn: "Create column", cancel: "Cancel", temporaryTip: (n: number, plural: string) => `Temporary column: tickets hide ${n} day${plural} after entering it. They are not deleted.`,
    moveLeft: (name: string) => `Move ${name} left`, moveRight: (name: string) => `Move ${name} right`,
    addTicketTo: (name: string) => `Add ticket to ${name}`, deleteBoard: "Delete this board?", boardDetails: "Board details",
    name: "Name", shortName: "Short name", saveBoard: "Save board", deleteBoardLabel: "Delete board", columns: "Columns",
    temporaryHelp: "Temporary: tickets auto-hide from the board N days after entering the column. They are not deleted.",
    order: "Order", columnOrder: "Column order", classic: "Classic", temporary: "Temporary", hideAfterDays: "Hide tickets after N days",
    days: "days", ticketTypes: "Ticket types", newType: "New type", color: "Color", addType: "Add type",
    deleteColumn: (name: string) => `Delete column "${name}"?`, deleteTicketType: (name: string) => `Delete ticket type "${name}"?`,
    columnType: (name: string) => `${name} type`, columnDays: (name: string) => `${name} hide after days`,
    save: (name: string) => `Save ${name}`, delete: (name: string) => `Delete ${name}`,
    close: "Close", title: "Title", description: "Description", type: "Type", noType: "No type", dueDate: "Due date", createTicket: "Create ticket",
  },
  es: {
    loading: "Cargando tablero...", boards: "Tableros", search: "Buscar tickets", searchPlaceholder: "Buscar tickets...",
    newTicket: "Nuevo ticket", closeSettings: "Cerrar configuración", boardSettings: "Configuración del tablero", excel: "Excel (.xlsx)",
    spreadsheet: "Hoja de cálculo de tickets", csv: "CSV", plainTable: "Tabla simple", jsonBackup: "Respaldo JSON",
    importPatio: "Importar a Patio en otro equipo", export: "Exportar", addColumn: "Agregar columna", columnName: "Nombre de columna",
    createColumn: "Crear columna", cancel: "Cancelar", temporaryTip: (n: number, plural: string) => `Columna temporal: los tickets se ocultan ${n} día${plural} después de entrar. No se eliminan.`,
    moveLeft: (name: string) => `Mover ${name} a la izquierda`, moveRight: (name: string) => `Mover ${name} a la derecha`,
    addTicketTo: (name: string) => `Agregar ticket a ${name}`, deleteBoard: "¿Eliminar este tablero?", boardDetails: "Detalles del tablero",
    name: "Nombre", shortName: "Nombre corto", saveBoard: "Guardar tablero", deleteBoardLabel: "Eliminar tablero", columns: "Columnas",
    temporaryHelp: "Temporal: los tickets se ocultan automáticamente del tablero N días después de entrar en la columna. No se eliminan.",
    order: "Orden", columnOrder: "Orden de columnas", classic: "Clásica", temporary: "Temporal", hideAfterDays: "Ocultar tickets después de N días",
    days: "días", ticketTypes: "Tipos de ticket", newType: "Nuevo tipo", color: "Color", addType: "Agregar tipo",
    deleteColumn: (name: string) => `¿Eliminar columna «${name}»?`, deleteTicketType: (name: string) => `¿Eliminar tipo de ticket «${name}»?`,
    columnType: (name: string) => `Tipo de ${name}`, columnDays: (name: string) => `Ocultar ${name} después de (días)`,
    save: (name: string) => `Guardar ${name}`, delete: (name: string) => `Eliminar ${name}`,
    close: "Cerrar", title: "Título", description: "Descripción", type: "Tipo", noType: "Sin tipo", dueDate: "Fecha de vencimiento", createTicket: "Crear ticket",
  },
};

export default function BoardView({
  id,
  refresh,
}: {
  id: string;
  refresh: number;
}) {
  const t = useText(text);
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
        {t.loading}
        <ErrorText error={error} />
      </div>
    );
  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <button className="back" onClick={() => navigate("/")}>
            <Icon name="arrowLeft" /> {t.boards}
          </button>
          <h1>{data.board.name}</h1>
        </div>
        <div className="actions">
          <input
            className="search"
            aria-label={t.search}
            placeholder={t.searchPlaceholder}
            value={query}
            onChange={(event) => search(event.target.value)}
          />
          <button className="primary" data-tour="new-ticket" onClick={() => setNewTicketStatus(data.statuses[0]?.id)}>
            <Icon name="plus" /> {t.newTicket}
          </button>
          <ExportMenu boardId={id} />
          <button onClick={() => setSettings(!settings)}>
            <Icon name={settings ? "x" : "settings"} />
            {settings ? t.closeSettings : t.boardSettings}
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
  ["xlsx", "excel", "spreadsheet"],
  ["csv", "csv", "plainTable"],
  ["json", "jsonBackup", "importPatio"],
];

function ExportMenu({ boardId }: { boardId: string }) {
  const t = useText(text);
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
        <Icon name="download" /> {t.export} <Icon name="chevronDown" />
      </button>
      {open && (
        <div className="menu-list" role="menu" aria-label={t.export} onMouseDown={(event) => event.preventDefault()}>
          {exportFormats.map(([format, label, hint]) => (
            <a
              key={format}
              role="menuitem"
              href={`/api/boards/${boardId}/export?format=${format}`}
              download=""
              // Defer so the browser starts the download before the item unmounts.
              onClick={() => setTimeout(close)}
            >
              <strong>{t[label as "excel" | "csv" | "jsonBackup"]}</strong>
              <small>{t[hint as "spreadsheet" | "plainTable" | "importPatio"]}</small>
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
  const t = useText(text);
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
          label={t.addColumn}
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
            aria-label={t.columnName}
            placeholder={t.columnName}
            value={newColumn}
            onChange={(event) => setNewColumn(event.target.value)}
          />
          <IconButton className="primary" icon="check" label={t.createColumn} />
          <IconButton
            type="button"
            className="ghost"
            icon="x"
            label={t.cancel}
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
  const t = useText(text);
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
  const tempTip = t.temporaryTip(status.hide_after_days, status.hide_after_days === 1 ? "" : "s");
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
            label={t.moveLeft(status.name)}
            disabled={index === 0}
            onClick={() => moveColumn(index - 1)}
          />
          <IconButton
            className="ghost"
            icon="chevronRight"
            label={t.moveRight(status.name)}
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
            done={status.type === "temporary"}
            onDrag={dragStart}
          />
        ))}
      </div>
      <IconButton
        className="add-ticket tip-up"
        icon="plus"
        label={t.addTicketTo(status.name)}
        onClick={() => onAdd(status.id)}
      />
    </section>
  );
}

function Card({
  ticket,
  types,
  done,
  onDrag,
}: {
  ticket: Ticket;
  types: TicketType[];
  done: boolean;
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
      {ticket.due_date && !done && <DueBadge date={ticket.due_date} />}
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
  const t = useText(text);
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
    if (!(await confirmDialog(t.deleteBoard))) return;
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
        <h2>{t.boardDetails}</h2>
        <div className="inline-form">
          <label>
            {t.name}
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <label>
            {t.shortName}
            <input
              value={shortName}
              onChange={(event) => setShortName(event.target.value)}
            />
          </label>
          <IconButton icon="check" label={t.saveBoard} onClick={saveBoard} />
          <IconButton
            className="danger-text"
            icon="trash"
            label={t.deleteBoardLabel}
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
  const t = useText(text);
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
      <h2>{t.columns}</h2>
      <small>
        {t.temporaryHelp}
      </small>
      {statuses.map((status) => (
        <StatusRow
          key={`${moves}-${status.id}`}
          status={status}
          onReload={onReload}
          onError={onError}
        />
      ))}
      <h3 className="preview-title">{t.order}</h3>
      <ol className="column-preview" aria-label={t.columnOrder}>
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
                label={t.moveLeft(status.name)}
                disabled={index === 0}
                onClick={() => move(index, index - 1)}
              />
              <IconButton
                className="ghost"
                icon="chevronRight"
                label={t.moveRight(status.name)}
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
  const t = useText(text);
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
    if (!(await confirmDialog(t.deleteColumn(status.name)))) return;
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
      <Picker
        label={t.columnType(status.name)}
        options={[
          { id: "classic", name: t.classic },
          { id: "temporary", name: t.temporary },
        ]}
        value={type}
        onChange={(value) => setType(value as Status["type"])}
      />
      <DaysField
        label={t.columnDays(status.name)}
        value={hideAfterDays}
        onChange={setHideAfterDays}
        hidden={type !== "temporary"}
      />
      <IconButton icon="check" label={t.save(status.name)} onClick={save} />
      <IconButton
        className="danger-text"
        icon="trash"
        label={t.delete(status.name)}
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
  const t = useText(text);
  return (
    <label
      className={`days-field ${hidden ? "is-hidden" : ""}`}
      data-tip={t.hideAfterDays}
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
      <span>{t.days}</span>
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
  const t = useText(text);
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
      <h2>{t.ticketTypes}</h2>
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
          {t.newType}
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label>
          {t.color}
          <input
            type="color"
            value={color}
            onChange={(event) => setColor(event.target.value)}
          />
        </label>
        <button onClick={add}>
          <Icon name="plus" /> {t.addType}
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
  const t = useText(text);
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
    if (!(await confirmDialog(t.deleteTicketType(type.name)))) return;
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
        aria-label={`${type.name} ${t.name.toLowerCase()}`}
        value={name}
        onChange={(event) => setName(event.target.value)}
      />
      <input
        aria-label={`${type.name} ${t.color.toLowerCase()}`}
        type="color"
        value={color}
        onChange={(event) => setColor(event.target.value)}
      />
      <IconButton icon="check" label={t.save(type.name)} onClick={save} />
      <IconButton
        className="danger-text"
        icon="trash"
        label={t.delete(type.name)}
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
  const t = useText(text);
  const ai = useAiCreate();
  const { board } = data;
  const created = useCreated(ai, async (since) =>
    (await api.get<SearchTicket[]>(`/api/tickets/search?board_id=${encodeURIComponent(board.id)}`))
      .filter((ticket) => ticket.created_at > since)
      .map((ticket) => ({ id: ticket.id, label: ticket.code, href: `/tickets/${ticket.code || ticket.id}` })),
  );
  const close = () => {
    void ai.cleanup();
    onDone();
  };
  const tabbed = !!ai.connectors?.length;
  const form = <TicketForm data={data} statusId={statusId} embedded={tabbed} onCreated={close} onClose={close} onError={onError} />;
  const status = data.statuses.find((item) => item.id === statusId)?.name ?? "";
  return (
    <div
      className="modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="new-ticket-title"
      // Esc inside a nested dialog (the connector picker) only closes that dialog.
      onKeyDown={(event) => event.key === "Escape" && !(event.target as Element).closest("dialog") && close()}
      onPointerDown={(event) => event.target === event.currentTarget && close()}
    >
      {tabbed ? (
        <section className="panel ticket-form">
          <div className="page-heading">
            <h2 id="new-ticket-title">{t.newTicket}</h2>
            <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={close} />
          </div>
          <CreateTabs kind="ticket" ai={ai} form={form}>
            {(back) => (
              <AskAI
                ai={ai}
                title={`${t.newTicket} · ${board.name}`}
                draft={ticketDraft[lang()](board.name, status)}
                context={{ icon: "boards", label: board.name, line: `Viewing board [${linkText(board.name)}](/boards/${board.id})` }}
                created={created}
                onBack={back}
                onOpen={(href) => {
                  close();
                  navigate(href);
                }}
                onDone={close}
              />
            )}
          </CreateTabs>
        </section>
      ) : (
        form
      )}
    </div>
  );
}

const ticketDraft = {
  en: (board: string, status: string) =>
    `Create a ticket on the ${board} board${status ? ` in ${status}` : ""}.\nTitle: <what needs doing>\nDetails: <context, steps, links>\nDue: <date or none>`,
  es: (board: string, status: string) =>
    `Crea un ticket en el tablero ${board}${status ? ` en ${status}` : ""}.\nTítulo: <qué hay que hacer>\nDetalles: <contexto, pasos, enlaces>\nVence: <fecha o ninguna>`,
};

export function NewTicketPage({ boardId }: { boardId: string }) {
  const t = useText(text);
  const [data, setData] = useState<BoardData>();
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    api.get<BoardData>(`/api/boards/${boardId}/data`).then(setData).catch(setError);
  }, [boardId]);
  const back = () => navigate(`/boards/${boardId}`);
  if (!data)
    return (
      <div className="loading">
        {t.loading}
        <ErrorText error={error} />
      </div>
    );
  return (
    <div className="stack">
      <button className="back" onClick={back}>
        <Icon name="arrowLeft" /> {data.board.name}
      </button>
      <ErrorText error={error} />
      <TicketForm
        data={data}
        onCreated={(ticket) => navigate(`/tickets/${ticket.id}`)}
        onClose={back}
        onError={setError}
      />
    </div>
  );
}

function TicketForm({
  data,
  statusId,
  embedded,
  onCreated,
  onClose,
  onError,
}: {
  data: BoardData;
  statusId?: string;
  embedded?: boolean;
  onCreated: (ticket: Ticket) => void;
  onClose: () => void;
  onError: (error: unknown) => void;
}) {
  const t = useText(text);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [type, setType] = useState("");
  const [dueDate, setDueDate] = useState("");
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    try {
      onCreated(
        await api.post<Ticket>(`/api/boards/${data.board.id}/tickets`, {
          title,
          description,
          ...(statusId ? { status_id: statusId } : {}),
          ticket_type_id: type || null,
          due_date: dueDate || null,
        }),
      );
    } catch (error) {
      onError(error);
    }
  }
  return (
    <form className={embedded ? "ticket-form" : "panel ticket-form"} onSubmit={submit}>
      {!embedded && (
        <div className="page-heading">
          <h2 id="new-ticket-title">{t.newTicket}</h2>
          <IconButton
            type="button"
            className="ghost"
            icon="x"
            label={t.close}
            onClick={onClose}
          />
        </div>
      )}
      <label>
        {t.title}
        <input
          autoFocus
          required
          value={title}
          onChange={(event) => setTitle(event.target.value)}
        />
      </label>
      <div className="field">
        {t.description}
        <MarkdownEditor label={t.description} value={description} onChange={setDescription} />
      </div>
      <div className="two">
        <div className="field">
          {t.type}
          <Picker label={t.type} empty={t.noType} options={data.ticket_types} value={type} onChange={setType} />
        </div>
        <label>
          {t.dueDate}
          <DueDateInput value={dueDate} onChange={setDueDate} />
        </label>
      </div>
      <button className="primary">
        <Icon name="plus" /> {t.createTicket}
      </button>
    </form>
  );
}
