import { useEffect, useState } from "react";
import AIBadge from "./AIBadge";
import Markdown from "./Markdown";
import {
  api,
  type BoardData,
  type History,
  type Ticket,
  type Transition,
} from "./api";
import { ErrorText, navigate } from "./App";
import { Icon } from "./icons";
import { lang, locale, useText } from "./i18n";

const text = { en: { loading: "Loading ticket...", print: "Print", back: "Back to ticket", unknown: "Unknown", type: "Type", noType: "No type", created: "Created", updated: "Updated", statusUpdated: "Status updated", dueDate: "Due date", none: "None", description: "Description", history: "History", when: "When", who: "Who", what: "What", transitions: "Status transitions", fromTo: "From → to", never: "Never" }, es: { loading: "Cargando ticket...", print: "Imprimir", back: "Volver al ticket", unknown: "Desconocido", type: "Tipo", noType: "Sin tipo", created: "Creado", updated: "Actualizado", statusUpdated: "Estado actualizado", dueDate: "Fecha de vencimiento", none: "Ninguna", description: "Descripción", history: "Historial", when: "Cuándo", who: "Quién", what: "Qué", transitions: "Transiciones de estado", fromTo: "De → a", never: "Nunca" } };

function date(value: string | null) {
  return value ? new Date(value).toLocaleString(locale()) : text[lang()].never;
}

type PrintData = {
  ticket: Ticket;
  board: BoardData;
  history: History[];
  transitions: Transition[];
};

export default function PrintTicket({
  id,
  refresh,
}: {
  id: string;
  refresh: number;
}) {
  const t = useText(text);
  const [data, setData] = useState<PrintData>();
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    async function load() {
      try {
        const ticket = await api.get<Ticket>(`/api/tickets/${id}`);
        const [board, history, transitions] = await Promise.all([
          api.get<BoardData>(`/api/boards/${ticket.board_id}/data`),
          api.get<History[]>(`/api/tickets/${ticket.id}/history`),
          api.get<Transition[]>(`/api/tickets/${ticket.id}/transitions`),
        ]);
        setData({ ticket, board, history, transitions });
      } catch (reason) {
        setError(reason);
      }
    }
    load();
  }, [id, refresh]);

  if (!data) {
    return (
      <div className="loading print-loading">
        {t.loading}
        <ErrorText error={error} />
      </div>
    );
  }

  const { ticket, board, history, transitions } = data;
  const status = board.statuses.find((item) => item.id === ticket.status_id);
  const type = board.ticket_types.find(
    (item) => item.id === ticket.ticket_type_id,
  );

  return (
    <article className="print-ticket">
      <div className="print-toolbar">
        <button
          onClick={() =>
            fetch("/api/print", { method: "POST" }).then(
              (res) => res.ok || window.print(),
              () => window.print(),
            )
          }
        >
          <Icon name="printer" /> {t.print}
        </button>
        <a
          href={`/tickets/${ticket.id}`}
          onClick={(event) => {
            event.preventDefault();
            navigate(`/tickets/${ticket.id}`);
          }}
        >
          <Icon name="arrowLeft" /> {t.back}
        </a>
      </div>
      <header className="print-heading">
        <div>
          <div className="eyebrow">{board.board.name}</div>
          <div className="ticket-code">{ticket.code}</div>
          <h1>{ticket.title}</h1>
        </div>
        <strong className="print-status">{status?.name || t.unknown}</strong>
      </header>
      <dl className="print-fields">
        <div>
          <dt>{t.type}</dt>
          <dd>{type?.name || t.noType}</dd>
        </div>
        <div>
          <dt>{t.created}</dt>
          <dd>{date(ticket.created_at)}</dd>
        </div>
        <div>
          <dt>{t.updated}</dt>
          <dd>{date(ticket.updated_at)}</dd>
        </div>
        <div>
          <dt>{t.statusUpdated}</dt>
          <dd>{date(ticket.status_updated_at)}</dd>
        </div>
        <div>
          <dt>{t.dueDate}</dt>
          <dd>{ticket.due_date ? date(ticket.due_date) : t.none}</dd>
        </div>
      </dl>
      <section className="print-section">
        <h2>{t.description}</h2>
        <div className="print-description">
          <Markdown text={ticket.description} />
        </div>
      </section>
      <section className="print-section">
        <h2>{t.history}</h2>
        <table>
          <thead>
            <tr>
              <th>{t.when}</th>
              <th>{t.who}</th>
              <th>{t.what}</th>
            </tr>
          </thead>
          <tbody>
            {history.map((item) => (
              <tr key={item.id}>
                <td>{date(item.created_at)}</td>
                <td>
                  {item.user_name} <AIBadge isAI={item.user_is_ai} />
                </td>
                <td>{item.content}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      <section className="print-section">
        <h2>{t.transitions}</h2>
        <table>
          <thead>
            <tr>
              <th>{t.when}</th>
              <th>{t.fromTo}</th>
              <th>{t.who}</th>
            </tr>
          </thead>
          <tbody>
            {transitions.map((item) => (
              <tr key={item.id}>
                <td>{date(item.moved_at)}</td>
                <td>
                  {item.from_status_name || t.created} → {item.to_status_name}
                </td>
                <td>
                  {item.user_name} <AIBadge isAI={item.user_is_ai} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </article>
  );
}
