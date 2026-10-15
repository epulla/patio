import { confirmDialog } from "./confirm";
import { useEffect, useState, type FormEvent } from "react";
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
import Picker from "./Picker";

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
      if (!item.all_day) return dateKey(new Date(item.starts_at)) === key;
      const start = item.starts_at.slice(0, 10);
      const end = item.ends_at?.slice(0, 10) ?? start;
      return start <= key && key <= end;
    });
    const tickets = data.tickets.filter(
      (ticket) => ticket.due_date?.slice(0, 10) === key,
    );
    // burnedout: assumed one chip per task per day, with a count, so hourly tasks stay readable
    const scheduled = new Map<string, { first: CalendarScheduled; count: number }>();
    for (const run of data.scheduled) {
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
          <p className="eyebrow">Schedule</p>
          <h1>Calendar</h1>
        </div>
        <div className="calendar-controls">
          <div className="join">
            <button onClick={() => setAnchor(new Date())}>Today</button>
            <IconButton
              icon="chevronLeft"
              label="Previous period"
              onClick={() => move(-1)}
            />
            <IconButton
              icon="chevronRight"
              label="Next period"
              onClick={() => move(1)}
            />
          </div>
          <div className="join">
            <button
              aria-pressed={view === "month"}
              onClick={() => setView("month")}
            >
              Month
            </button>
            <button
              aria-pressed={view === "week"}
              onClick={() => setView("week")}
            >
              Week
            </button>
          </div>
          <label>
            Board
            <Picker label="Board" empty="All boards" options={boards} value={boardID} onChange={setBoardID} />
          </label>
        </div>
      </div>
      <h2 className="calendar-period">
        {view === "week"
          ? `${visibleStart.toLocaleDateString()} – ${visibleEnd.toLocaleDateString()}`
          : anchor.toLocaleDateString(undefined, {
              month: "long",
              year: "numeric",
            })}
      </h2>
      <ErrorText error={error} />
      <div className={`calendar-grid ${view === "week" ? "week-view" : ""}`}>
        {["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((name) => (
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
                  label={`Add item on ${key}`}
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
                        {new Date(item.starts_at).toLocaleTimeString([], {
                          hour: "numeric",
                          minute: "2-digit",
                        })}{" "}
                      </time>
                    )}
                    <span className="calendar-kind">
                      {item.kind === "reminder" ? "Reminder" : "Event"}
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
  const time = new Date(run.at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  const status = run.status === "skipped" ? "Skipped" : run.late ? "Ran late" : run.status === "failed" ? "Failed" : "";
  return (
    <button
      className="calendar-chip scheduled"
      title={count > 1 ? `${count} runs this day, first at ${time}` : undefined}
      onClick={() => navigate(run.chat_id && count === 1 ? `/ai/chats/${run.chat_id}` : `/ai/scheduled/${run.task_id}`)}
    >
      <time>{time} </time>
      <span className="calendar-kind">AI task</span> {run.title}
      {count > 1 && ` ×${count}`}
      {status && ` · ${status}`}
    </button>
  );
}

function TicketChip({ ticket }: { ticket: Ticket }) {
  return (
    <button
      className="calendar-chip due"
      onClick={() => navigate(`/tickets/${ticket.id}`)}
    >
      <span className="calendar-kind">Due</span> {ticket.code} {ticket.title}
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
  const [draft, setDraft] = useState(() => initialDraft(item, date));
  const [error, setError] = useState<unknown>();
  const [saving, setSaving] = useState(false);

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
    if (!item || !(await confirmDialog("Delete this calendar item?"))) return;
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
    <section className="panel calendar-form-panel">
      <div className="page-heading">
        <h2>{item ? "Edit calendar item" : "New calendar item"}</h2>
        <IconButton className="ghost" icon="x" label="Close" onClick={onCancel} />
      </div>
      <form className="calendar-form" onSubmit={submit}>
        <label>
          Kind
          <Picker
            label="Kind"
            options={[
              { id: "event", name: "Event" },
              { id: "reminder", name: "Reminder" },
            ]}
            value={draft.kind}
            onChange={(kind) => setDraft({ ...draft, kind: kind as Draft["kind"] })}
          />
        </label>
        <label>
          Title
          <input
            required
            value={draft.title}
            onChange={(event) =>
              setDraft({ ...draft, title: event.target.value })
            }
          />
        </label>
        <label className="calendar-description">
          Description
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
          All day
        </label>
        <label>
          Start
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
          End (optional)
          <input
            type={draft.all_day ? "date" : "datetime-local"}
            value={draft.ends_at}
            onChange={(event) =>
              setDraft({ ...draft, ends_at: event.target.value })
            }
          />
        </label>
        <label>
          Board (optional)
          <Picker
            label="Board"
            empty="No board"
            options={boards}
            value={draft.board_id}
            onChange={(board_id) => setDraft({ ...draft, board_id })}
          />
        </label>
        <div className="calendar-form-actions">
          <button className="primary" disabled={saving}>
            <Icon name="check" /> Save
          </button>
          {item && (
            <button
              type="button"
              className="danger"
              disabled={saving}
              onClick={remove}
            >
              <Icon name="trash" /> Delete
            </button>
          )}
        </div>
      </form>
      <ErrorText error={error} />
    </section>
  );
}
