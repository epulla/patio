import { useEffect, useState } from "react";
import { api, type Board, type CalendarResponse, type Ticket } from "./api";
import { ErrorText, navigate } from "./App";
import { locale, useText } from "./i18n";
import Picker from "./Picker";

type DashboardData = {
  days: { date: string; done: number; cancelled: number }[];
  created: number;
  done: number;
  cancelled: number;
  boards: { id: string; name: string; created: number; done: number; cancelled: number }[];
  overdue: Ticket[];
  due_soon: Ticket[];
  previous?: { done: number; created: number };
};

const text = {
  en: {
    overview: "Overview", dashboard: "Dashboard", done: "Done", cancelled: "Cancelled", created: "Created", overdue: "Overdue", dueSoon: "Due in 7 days", today: "Events today", openTickets: "Open tickets", vsPrevious: (pct: number) => `${pct >= 0 ? "↑" : "↓"} ${Math.abs(pct)}% vs previous`, donePct: "Done %",
    lastDays: (n: number) => `Last ${n} days`, perDay: "Done per day", byBoard: "By board", board: "Board", tickets: "Tickets due", upcoming: "Upcoming 7 days",
    nothingDue: "Nothing overdue or due soon.", nothingUpcoming: "No events or scheduled runs.", bar: (date: string, done: number, cancelled: number) => `${date}: ${done} done, ${cancelled} cancelled`,
    chart: (done: number, cancelled: number) => `${done} done and ${cancelled} cancelled in this range`, range: "Range", allBoards: "All boards", aiTask: "AI task", event: "Event", reminder: "Reminder", noActivity: "No activity in this range.",
  },
  es: {
    overview: "Resumen", dashboard: "Panel", done: "Hechos", cancelled: "Cancelados", created: "Creados", overdue: "Vencidos", dueSoon: "Vencen en 7 días", today: "Eventos hoy", openTickets: "Tickets abiertos", vsPrevious: (pct: number) => `${pct >= 0 ? "↑" : "↓"} ${Math.abs(pct)}% vs. periodo anterior`, donePct: "% hechos",
    lastDays: (n: number) => `Últimos ${n} días`, perDay: "Hechos por día", byBoard: "Por tablero", board: "Tablero", tickets: "Tickets por vencer", upcoming: "Próximos 7 días",
    nothingDue: "Nada vencido ni por vencer.", nothingUpcoming: "Sin eventos ni ejecuciones programadas.", bar: (date: string, done: number, cancelled: number) => `${date}: ${done} hechos, ${cancelled} cancelados`,
    chart: (done: number, cancelled: number) => `${done} hechos y ${cancelled} cancelados en este periodo`, range: "Periodo", allBoards: "Todos los tableros", aiTask: "Tarea de IA", event: "Evento", reminder: "Recordatorio", noActivity: "Sin actividad en este periodo.",
  },
};

const shortDate = (date: string) => new Date(`${date}T00:00`).toLocaleDateString(locale(), { month: "short", day: "numeric" });
const change = (current: number, previous?: number) => (previous ? Math.round(((current - previous) / previous) * 100) : undefined);

const dateKey = (date: Date) => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;

export default function Dashboard({ refresh }: { refresh: number }) {
  const t = useText(text);
  const [days, setDays] = useState(30);
  const [boardID, setBoardID] = useState("");
  const [boards, setBoards] = useState<Board[]>([]);
  const [data, setData] = useState<DashboardData>();
  const [calendar, setCalendar] = useState<CalendarResponse>({ items: [], tickets: [], scheduled: [] });
  const [error, setError] = useState<unknown>();
  const [hover, setHover] = useState<number>();
  const now = new Date();
  const today = dateKey(now);

  useEffect(() => {
    api.get<Board[]>("/api/boards").then(setBoards).catch(setError);
  }, [refresh]);

  useEffect(() => {
    let active = true;
    const board = boardID ? `&board_id=${encodeURIComponent(boardID)}` : "";
    const week = new Date();
    week.setDate(week.getDate() + 7);
    Promise.all([
      api.get<DashboardData>(`/api/dashboard?days=${days}${board}`),
      api.get<CalendarResponse>(`/api/calendar?from=${today}&to=${dateKey(week)}${board}`),
    ])
      .then(([dashboard, upcoming]) => {
        if (!active) return;
        setData(dashboard);
        setCalendar(upcoming);
        setError(undefined);
      })
      .catch((reason) => active && setError(reason));
    return () => {
      active = false;
    };
  }, [refresh, today, days, boardID]);

  const nextRuns = calendar.scheduled.filter((run, i, all) => new Date(run.at) >= now && all.findIndex((other) => other.task_id === run.task_id && new Date(other.at) >= now) === i);
  const upcoming = [
    ...calendar.items.map((item) => ({ key: item.id, at: item.starts_at, title: item.title, kind: item.kind as string, allDay: item.all_day, to: "/calendar" })),
    ...nextRuns.map((run) => ({ key: run.task_id, at: run.at, title: run.title, kind: "scheduled", allDay: false, to: `/ai/scheduled/${run.task_id}` })),
  ].sort((a, b) => a.at.localeCompare(b.at));
  const todayCount = upcoming.filter((item) => (item.allDay ? item.at.slice(0, 10) : dateKey(new Date(item.at))) <= today).length;
  const max = Math.max(1, ...(data?.days ?? []).map((day) => day.done + day.cancelled));
  const tickEvery = days <= 7 ? 1 : days <= 30 ? 7 : 14;
  const gridlines = max > 1 ? [max, Math.round(max / 2)] : [max];
  const hovered = hover === undefined ? undefined : data?.days[hover];
  const toTickets = () => document.getElementById("dashboard-tickets")?.scrollIntoView({ behavior: "smooth", block: "start" });
  const when = (at: string, allDay: boolean) =>
    allDay ? new Date(`${at.slice(0, 10)}T00:00`).toLocaleDateString(locale(), { weekday: "short", day: "numeric", month: "short" }) : new Date(at).toLocaleString(locale(), { weekday: "short", hour: "numeric", minute: "2-digit" });

  return (
    <div className="stack dashboard-page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">{t.overview}</p>
          <h1>{t.dashboard}</h1>
        </div>
        <div className="calendar-controls">
          <div className="join" role="group" aria-label={t.range}>
            {[7, 30, 90].map((n) => (
              <button key={n} aria-pressed={days === n} onClick={() => setDays(n)}>
                {t.lastDays(n)}
              </button>
            ))}
          </div>
          <label>
            {t.board}
            <Picker label={t.board} empty={t.allBoards} options={boards} value={boardID} onChange={setBoardID} />
          </label>
        </div>
      </div>
      <ErrorText error={error} />
      {data && (
        <>
          <div className="dashboard-stats">
            {[
              { label: t.done, value: data.done, hint: t.lastDays(days), delta: change(data.done, data.previous?.done) },
              { label: t.created, value: data.created, hint: t.lastDays(days), delta: change(data.created, data.previous?.created) },
              { label: t.overdue, value: data.overdue.length, hint: t.openTickets, onClick: toTickets },
              { label: t.dueSoon, value: data.due_soon.length, hint: t.openTickets, onClick: toTickets },
              { label: t.today, value: todayCount, hint: t.upcoming },
            ].map(({ label, value, hint, delta, onClick }) => {
              const body = (
                <>
                  <span className="eyebrow">{label}</span>
                  <strong>{value}</strong>
                  <small>
                    {hint}
                    {delta !== undefined && <span className={`dashboard-delta ${delta >= 0 ? "up" : "down"}`}> · {t.vsPrevious(delta)}</span>}
                  </small>
                </>
              );
              const className = `panel dashboard-stat ${label === t.overdue && value > 0 ? "alert" : ""}`;
              return onClick ? (
                <button type="button" className={`${className} clickable`} key={label} onClick={onClick}>
                  {body}
                </button>
              ) : (
                <div className={className} key={label}>
                  {body}
                </div>
              );
            })}
          </div>
          <section className="panel">
            <div className="dashboard-section-head">
              <h2>{t.perDay}</h2>
              <span className="dashboard-legend">
                <span className="done">{t.done}</span>
                <span className="cancelled">{t.cancelled}</span>
              </span>
            </div>
            <div className="dashboard-chart" role="img" aria-label={t.chart(data.done, data.cancelled)} onMouseLeave={() => setHover(undefined)}>
              {gridlines.map((value) => (
                <span className="dashboard-gridline" key={value} style={{ bottom: `${(value / max) * 100}%` }} aria-hidden="true">
                  <span>{value}</span>
                </span>
              ))}
              {data.days.map((day, i) => (
                <div className="dashboard-bar" key={day.date} onMouseEnter={() => setHover(i)}>
                  <span className="cancelled" style={{ height: `${(day.cancelled / max) * 100}%` }} />
                  <span className="done" style={{ height: `${(day.done / max) * 100}%` }} />
                </div>
              ))}
              {hovered && (
                <div className="dashboard-tooltip" style={{ left: `${((hover! + 0.5) / data.days.length) * 100}%` }} aria-hidden="true">
                  {t.bar(shortDate(hovered.date), hovered.done, hovered.cancelled)}
                </div>
              )}
            </div>
            <div className="dashboard-axis" aria-hidden="true">
              {data.days.map((day, i) => (
                <span key={day.date}>{(data.days.length - 1 - i) % tickEvery === 0 ? shortDate(day.date) : ""}</span>
              ))}
            </div>
          </section>
          <section className="panel">
            <h2>{t.byBoard}</h2>
            {data.boards.length === 0 ? (
              <p className="dashboard-empty">{t.noActivity}</p>
            ) : (
              <table className="dashboard-table">
                <thead>
                  <tr>
                    <th scope="col">{t.board}</th>
                    <th scope="col">{t.created}</th>
                    <th scope="col">{t.done}</th>
                    <th scope="col">{t.cancelled}</th>
                    <th scope="col">{t.donePct}</th>
                  </tr>
                </thead>
                <tbody>
                  {[...data.boards].sort((a, b) => b.done - a.done).map((board) => (
                    <tr key={board.id}>
                      <th scope="row">
                        <a href={`/boards/${board.id}`} onClick={(event) => (event.preventDefault(), navigate(`/boards/${board.id}`))}>
                          {board.name}
                        </a>
                      </th>
                      <td>{board.created}</td>
                      <td>{board.done}</td>
                      <td>{board.cancelled}</td>
                      <td>{board.created ? `${Math.round((board.done / board.created) * 100)}%` : "–"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
          <div className="dashboard-lists">
            <section className="panel" id="dashboard-tickets">
              <h2>{t.tickets}</h2>
              {data.overdue.length + data.due_soon.length === 0 ? (
                <p className="dashboard-empty">{t.nothingDue}</p>
              ) : (
                <ul className="dashboard-list">
                  {[...data.overdue, ...data.due_soon].map((ticket) => (
                    <li key={ticket.id}>
                      <button className="ghost" onClick={() => navigate(`/tickets/${ticket.id}`)}>
                        <span className="ticket-code">{ticket.code}</span>
                        <span className="dashboard-item-title">{ticket.title}</span>
                        <span className={ticket.due_date! < today ? "dashboard-due overdue" : "dashboard-due"}>{when(ticket.due_date!, true)}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            <section className="panel">
              <h2>{t.upcoming}</h2>
              {upcoming.length === 0 ? (
                <p className="dashboard-empty">{t.nothingUpcoming}</p>
              ) : (
                <ul className="dashboard-list">
                  {upcoming.map((item) => (
                    <li key={`${item.kind}-${item.key}`}>
                      <button className="ghost" onClick={() => navigate(item.to)}>
                        <span className={`calendar-chip ${item.kind}`}>{item.kind === "scheduled" ? t.aiTask : item.kind === "reminder" ? t.reminder : t.event}</span>
                        <span className="dashboard-item-title">{item.title}</span>
                        <span className="dashboard-due">{when(item.at, item.allDay)}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </div>
        </>
      )}
    </div>
  );
}
