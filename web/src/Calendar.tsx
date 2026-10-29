import { confirmDialog } from "./confirm";
import { useEffect, useRef, useState, type FormEvent } from "react";
import AIBadge from "./AIBadge";
import {
  api,
  type Board,
  type CalendarItem,
  type CalendarResponse,
  type CalendarScheduled,
  type Ticket,
} from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";
import Menu from "./Menu";
import Picker from "./Picker";
import { locale, useText } from "./i18n";

const text = { en: { schedule: "Schedule", calendar: "Calendar", today: "Today", previous: "Previous period", next: "Next period", month: "Month", week: "Week", board: "Board", allBoards: "All boards", addOn: (date: string) => `Add item on ${date}`, reminder: "Reminder", event: "Event", skipped: "Skipped", ranLate: "Ran late", failed: "Failed", waiting: "Waiting", runsFirst: (count: number, time: string) => `${count} runs this day, first at ${time}`, aiTask: "AI task", due: "Due", edit: "Edit calendar item", new: "New calendar item", close: "Close", kind: "Kind", title: "Title", description: "Description", allDay: "All day", start: "Start", endOptional: "End (optional)", boardOptional: "Board (optional)", noBoard: "No board", save: "Save", delete: "Delete", confirmDelete: "Delete this calendar item?", show: "Show", events: "Events", reminders: "Reminders", dueDates: "Ticket due dates", aiTasks: "AI tasks" }, es: { schedule: "Agenda", calendar: "Calendario", today: "Hoy", previous: "Periodo anterior", next: "Periodo siguiente", month: "Mes", week: "Semana", board: "Tablero", allBoards: "Todos los tableros", addOn: (date: string) => `Agregar elemento el ${date}`, reminder: "Recordatorio", event: "Evento", skipped: "Omitida", ranLate: "Se ejecutó tarde", failed: "Fallida", waiting: "En espera", runsFirst: (count: number, time: string) => `${count} ejecuciones hoy; primera a las ${time}`, aiTask: "Tarea de IA", due: "Vence", edit: "Editar elemento del calendario", new: "Nuevo elemento del calendario", close: "Cerrar", kind: "Tipo", title: "Título", description: "Descripción", allDay: "Todo el día", start: "Inicio", endOptional: "Fin (opcional)", boardOptional: "Tablero (opcional)", noBoard: "Sin tablero", save: "Guardar", delete: "Eliminar", confirmDelete: "¿Eliminar este elemento del calendario?", show: "Mostrar", events: "Eventos", reminders: "Recordatorios", dueDates: "Vencimientos de tickets", aiTasks: "Tareas de IA" } };

export type Shown = { event: boolean; reminder: boolean; due: boolean; scheduled: boolean };

export function loadShown(key = "calendar-shown"): Shown {
  const all = { event: true, reminder: true, due: true, scheduled: true };
  try {
    return { ...all, ...JSON.parse(localStorage.getItem(key) ?? "{}") };
  } catch {
    return all;
  }
}

type Draft = {
  kind: "event" | "reminder";
  title: string;
  description: string;
  all_day: boolean;
  starts_at: string;
  ends_at: string;
  board_id: string;
};

function dateKey(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

function dateFromKey(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return new Date(year, month - 1, day);
}

function addDays(date: Date, amount: number) {
  const result = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  result.setDate(result.getDate() + amount);
  return result;
}

function monday(date: Date) {
  const result = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  result.setDate(result.getDate() - ((result.getDay() + 6) % 7));
  return result;
}

function dateTimeValue(value: string) {
  const date = new Date(value);
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 16);
}

function initialDraft(item: CalendarItem | null, date: string): Draft {
  return {
    kind: item?.kind ?? "event",
    title: item?.title ?? "",
    description: item?.description ?? "",
    all_day: item?.all_day ?? true,
    starts_at: item
      ? item.all_day
        ? item.starts_at.slice(0, 10)
        : dateTimeValue(item.starts_at)
      : date,
    ends_at: item?.ends_at
      ? item.all_day
        ? item.ends_at.slice(0, 10)
        : dateTimeValue(item.ends_at)
      : "",
    board_id: item?.board_id ?? "",
  };
}

export default function Calendar({ refresh }: { refresh: number }) {
  const t = useText(text);
  const [anchor, setAnchor] = useState(() => new Date());
  const [view, setView] = useState<"month" | "week">("month");
  const [boardID, setBoardID] = useState("");
  const [boards, setBoards] = useState<Board[]>([]);
  const [data, setData] = useState<CalendarResponse>({
    items: [],
    tickets: [],
    scheduled: [],
  });
  const [error, setError] = useState<unknown>();
  const [editing, setEditing] = useState<CalendarItem | null | undefined>();
  const [createDate, setCreateDate] = useState("");
  const [reload, setReload] = useState(0);
  const [shown, setShown] = useState(() => loadShown());

  function toggle(kind: keyof Shown, value: boolean) {
    const next = { ...shown, [kind]: value };
    setShown(next);
    localStorage.setItem("calendar-shown", JSON.stringify(next));
  }

  const visibleStart =
    view === "week"
      ? monday(anchor)
      : monday(new Date(anchor.getFullYear(), anchor.getMonth(), 1));
  const visibleEnd =
    view === "week"
      ? addDays(visibleStart, 6)
      : addDays(
          monday(new Date(anchor.getFullYear(), anchor.getMonth() + 1, 0)),
          6,
        );
  const today = dateKey(new Date());
  const days: Date[] = [];
  for (let day = visibleStart; day <= visibleEnd; day = addDays(day, 1))
    days.push(day);

  useEffect(() => {
    api.get<Board[]>("/api/boards").then(setBoards).catch(setError);
  }, [refresh]);

  useEffect(() => {
    let active = true;
    const from = dateKey(addDays(visibleStart, -1));
    const to = dateKey(addDays(visibleEnd, 1));
    const query = new URLSearchParams({ from, to });
    if (boardID) query.set("board_id", boardID);
    api
      .get<CalendarResponse>(`/api/calendar?${query}`)
      .then((result) => {
        if (active) {
          setData(result);
          setError(undefined);
        }
      })
      .catch((reason) => {
        if (active) setError(reason);
      });
    return () => {
      active = false;
    };
  }, [refresh, reload, visibleStart.getTime(), visibleEnd.getTime(), boardID]);

  function move(amount: number) {
    setAnchor(
      view === "week"
        ? addDays(anchor, amount * 7)
        : new Date(anchor.getFullYear(), anchor.getMonth() + amount, 1),
    );
  }

  function createOn(date: string) {
    setCreateDate(date);
    setEditing(null);
  }

  function dayItems(key: string) {
    const items = data.items.filter((item) => {
      if (!shown[item.kind]) return false;
      if (!item.all_day) return dateKey(new Date(item.starts_at)) === key;
      const start = item.starts_at.slice(0, 10);
      const end = item.ends_at?.slice(0, 10) ?? start;
      return start <= key && key <= end;
    });
    const tickets = data.tickets.filter(
      (ticket) => shown.due && ticket.due_date?.slice(0, 10) === key,
    );
    // burnedout: assumed one chip per task per day, with a count, so hourly tasks stay readable
    const scheduled = new Map<string, { first: CalendarScheduled; count: number }>();
    for (const run of shown.scheduled ? data.scheduled : []) {
      if (dateKey(new Date(run.at)) !== key) continue;
      const group = scheduled.get(run.task_id);
      if (group) group.count++;
      else scheduled.set(run.task_id, { first: run, count: 1 });
    }
    return { items, tickets, scheduled: [...scheduled.values()] };
  }

  return (
    <div className="stack calendar-page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">{t.schedule}</p>
          <h1>{t.calendar}</h1>
        </div>
        <div className="calendar-controls">
          <div className="join">
            <button onClick={() => setAnchor(new Date())}>{t.today}</button>
            <IconButton
              icon="chevronLeft"
              label={t.previous}
              onClick={() => move(-1)}
            />
            <IconButton
              icon="chevronRight"
              label={t.next}
              onClick={() => move(1)}
            />
          </div>
          <div className="join">
            <button
              aria-pressed={view === "month"}
              onClick={() => setView("month")}
            >
              {t.month}
            </button>
            <button
              aria-pressed={view === "week"}
              onClick={() => setView("week")}
            >
              {t.week}
            </button>
          </div>
          <label>
            {t.board}
            <Picker label={t.board} empty={t.allBoards} options={boards} value={boardID} onChange={setBoardID} />
          </label>
          <Menu label={t.show} buttonClassName="icon" button={<Icon name="filter" />}>
            {() =>
              ([["event", t.events], ["reminder", t.reminders], ["due", t.dueDates], ["scheduled", t.aiTasks]] as const).map(([kind, label]) => (
                <button key={kind} type="button" role="menuitemcheckbox" aria-checked={shown[kind]} className="setting-row" onClick={() => toggle(kind, !shown[kind])}>
                  <span className="setting-text">{label}</span>
                  <span className="switch-track" aria-hidden="true" />
                </button>
              ))
            }
          </Menu>
        </div>
      </div>
      <h2 className="calendar-period">
        {view === "week"
          ? `${visibleStart.toLocaleDateString(locale())} – ${visibleEnd.toLocaleDateString(locale())}`
          : anchor.toLocaleDateString(locale(), {
              month: "long",
              year: "numeric",
            })}
      </h2>
      <ErrorText error={error} />
      <div className={`calendar-grid ${view === "week" ? "week-view" : ""}`}>
        {(locale().startsWith("es") ? ["lun", "mar", "mié", "jue", "vie", "sáb", "dom"] : ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]).map((name) => (
          <div className="calendar-weekday" key={name}>
            {name}
          </div>
        ))}
        {days.map((day) => {
          const key = dateKey(day);
          const { items, tickets, scheduled } = dayItems(key);
          return (
            <section
              className={`calendar-day ${view === "month" && day.getMonth() !== anchor.getMonth() ? "outside-month" : ""} ${key === today ? "today" : ""}`}
              key={key}
            >
              <div className="calendar-day-head">
                <time dateTime={key}>{day.getDate()}</time>
                <IconButton
                  className="ghost"
                  icon="plus"
                  label={t.addOn(key)}
                  onClick={() => createOn(key)}
                />
              </div>
              <div className="calendar-chips">
                {tickets.map((ticket) => (
                  <TicketChip key={ticket.id} ticket={ticket} />
                ))}
                {items.map((item) => (
                  <button
                    className={`calendar-chip ${item.kind}`}
                    key={item.id}
                    onClick={() => setEditing(item)}
                  >
                    {!item.all_day && (
                      <time>
                        {new Date(item.starts_at).toLocaleTimeString(locale(), {
                          hour: "numeric",
                          minute: "2-digit",
                        })}{" "}
                      </time>
                    )}
                    <span className="calendar-kind">
                      {item.kind === "reminder" ? t.reminder : t.event}
                    </span>{" "}
                    {item.title} <AIBadge isAI={item.created_by_is_ai} />
                  </button>
                ))}
                {scheduled.map(({ first, count }) => (
                  <ScheduledChip key={first.task_id} run={first} count={count} />
                ))}
              </div>
            </section>
          );
        })}
      </div>
      {editing !== undefined && (
        <CalendarForm
          item={editing}
          date={createDate}
          boards={boards}
          onCancel={() => setEditing(undefined)}
          onSaved={() => {
            setEditing(undefined);
            setReload((value) => value + 1);
          }}
        />
      )}
    </div>
  );
}

function ScheduledChip({ run, count }: { run: CalendarScheduled; count: number }) {
  const t = useText(text);
  const time = new Date(run.at).toLocaleTimeString(locale(), { hour: "numeric", minute: "2-digit" });
  const status = run.status === "skipped" ? t.skipped : run.late ? t.ranLate : run.status === "failed" ? t.failed : run.status === "waiting" ? t.waiting : "";
  return (
    <button
      className="calendar-chip scheduled"
      title={count > 1 ? t.runsFirst(count, time) : undefined}
      onClick={() => navigate(run.chat_id && count === 1 ? `/ai/chats/${run.chat_id}` : `/ai/scheduled/${run.task_id}`)}
    >
      <time>{time} </time>
      <span className="calendar-kind">{t.aiTask}</span> {run.title}
      {count > 1 && ` ×${count}`}
      {status && ` · ${status}`}
    </button>
  );
}

function TicketChip({ ticket }: { ticket: Ticket }) {
  const t = useText(text);
  return (
    <button
      className="calendar-chip due"
      onClick={() => navigate(`/tickets/${ticket.id}`)}
    >
      <span className="calendar-kind">{t.due}</span> {ticket.code} {ticket.title}
    </button>
  );
}

function CalendarForm({
  item,
  date,
  boards,
  onCancel,
  onSaved,
}: {
  item: CalendarItem | null;
  date: string;
  boards: Board[];
  onCancel: () => void;
  onSaved: () => void;
}) {
  const t = useText(text);
  const [draft, setDraft] = useState(() => initialDraft(item, date));
  const [error, setError] = useState<unknown>();
  const [saving, setSaving] = useState(false);
  const dialog = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
  }, []);

  function toggleAllDay(allDay: boolean) {
    setDraft((current) => ({
      ...current,
      all_day: allDay,
      starts_at: allDay
        ? current.starts_at.slice(0, 10)
        : `${current.starts_at.slice(0, 10)}T09:00`,
      ends_at: current.ends_at
        ? allDay
          ? current.ends_at.slice(0, 10)
          : `${current.ends_at.slice(0, 10)}T09:00`
        : "",
    }));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    const toISO = (value: string) =>
      draft.all_day ? value : new Date(value).toISOString();
    const payload = {
      kind: draft.kind,
      title: draft.title.trim(),
      description: draft.description,
      starts_at: toISO(draft.starts_at),
      ends_at: draft.ends_at ? toISO(draft.ends_at) : null,
      all_day: draft.all_day,
      board_id: draft.board_id || null,
    };
    try {
      if (item)
        await api.put<CalendarItem>(`/api/calendar-items/${item.id}`, payload);
      else await api.post<CalendarItem>("/api/calendar-items", payload);
      onSaved();
    } catch (reason) {
      setError(reason);
    } finally {
      setSaving(false);
    }
  }

  async function remove() {
    if (!item || !(await confirmDialog(t.confirmDelete))) return;
    setSaving(true);
    setError(undefined);
    try {
      await api.delete(`/api/calendar-items/${item.id}`);
      onSaved();
    } catch (reason) {
      setError(reason);
    } finally {
      setSaving(false);
    }
  }

  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="calendar-form-title" onClose={onCancel}>
      <section className="panel create-modal calendar-form-panel">
        <div className="page-heading">
          <h2 id="calendar-form-title">{item ? t.edit : t.new}</h2>
          <IconButton type="button" className="ghost" icon="x" label={t.close} onClick={() => dialog.current?.close()} />
        </div>
        <form className="calendar-form" onSubmit={submit}>
          <label>
            {t.kind}
            <Picker
              label={t.kind}
              options={[
                { id: "event", name: t.event },
                { id: "reminder", name: t.reminder },
              ]}
              value={draft.kind}
              onChange={(kind) => setDraft({ ...draft, kind: kind as Draft["kind"] })}
            />
          </label>
          <label>
            {t.title}
            <input
              required
              value={draft.title}
              onChange={(event) =>
                setDraft({ ...draft, title: event.target.value })
              }
            />
          </label>
          <label className="calendar-description">
            {t.description}
            <textarea
              value={draft.description}
              onChange={(event) =>
                setDraft({ ...draft, description: event.target.value })
              }
            />
          </label>
          <label className="calendar-check">
            <input
              type="checkbox"
              checked={draft.all_day}
              onChange={(event) => toggleAllDay(event.target.checked)}
            />
            {t.allDay}
          </label>
          <label>
            {t.start}
            <input
              required
              type={draft.all_day ? "date" : "datetime-local"}
              value={draft.starts_at}
              onChange={(event) =>
                setDraft({ ...draft, starts_at: event.target.value })
              }
            />
          </label>
          <label>
            {t.endOptional}
            <input
              type={draft.all_day ? "date" : "datetime-local"}
              value={draft.ends_at}
              onChange={(event) =>
                setDraft({ ...draft, ends_at: event.target.value })
              }
            />
          </label>
          <label>
            {t.boardOptional}
            <Picker
              label={t.board}
              empty={t.noBoard}
              options={boards}
              value={draft.board_id}
              onChange={(board_id) => setDraft({ ...draft, board_id })}
            />
          </label>
          <div className="calendar-form-actions">
            <button className="primary" disabled={saving}>
              <Icon name="check" /> {t.save}
            </button>
            {item && (
              <button
                type="button"
                className="danger"
                disabled={saving}
                onClick={remove}
              >
                <Icon name="trash" /> {t.delete}
              </button>
            )}
          </div>
        </form>
        <ErrorText error={error} />
      </section>
    </dialog>
  );
}
