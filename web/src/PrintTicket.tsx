import { useEffect, useState } from "react";
import DOMPurify from "dompurify";
import AIBadge from "./AIBadge";
import {
  api,
  type BoardData,
  type History,
  type Ticket,
  type Transition,
} from "./api";
import { ErrorText, formatDate, navigate } from "./App";

type PrintData = {
  ticket: Ticket;
  board: BoardData;
  history: History[];
  transitions: Transition[];
};

export default function PrintTicket({ id }: { id: string }) {
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
  }, [id]);

  if (!data) {
    return (
      <div className="loading print-loading">
        Loading ticket...
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
          Print
        </button>
        <a
          href={`/tickets/${ticket.id}`}
          onClick={(event) => {
            event.preventDefault();
            navigate(`/tickets/${ticket.id}`);
          }}
        >
          Back to ticket
        </a>
      </div>
      <header className="print-heading">
        <div>
          <div className="eyebrow">{board.board.name}</div>
          <div className="ticket-code">{ticket.code}</div>
          <h1>{ticket.title}</h1>
        </div>
        <strong className="print-status">{status?.name || "Unknown"}</strong>
      </header>
      <dl className="print-fields">
        <div>
          <dt>Type</dt>
          <dd>{type?.name || "No type"}</dd>
        </div>
        <div>
          <dt>Created</dt>
          <dd>{formatDate(ticket.created_at)}</dd>
        </div>
        <div>
          <dt>Updated</dt>
          <dd>{formatDate(ticket.updated_at)}</dd>
        </div>
        <div>
          <dt>Status updated</dt>
          <dd>{formatDate(ticket.status_updated_at)}</dd>
        </div>
        <div>
          <dt>Due date</dt>
          <dd>{ticket.due_date ? formatDate(ticket.due_date) : "None"}</dd>
        </div>
      </dl>
      <section className="print-section">
        <h2>Description</h2>
        <div
          className="print-description"
          dangerouslySetInnerHTML={{
            __html: DOMPurify.sanitize(ticket.description || ""),
          }}
        />
      </section>
      <section className="print-section">
        <h2>History</h2>
        <table>
          <thead>
            <tr>
              <th>When</th>
              <th>Who</th>
              <th>What</th>
            </tr>
          </thead>
          <tbody>
            {history.map((item) => (
              <tr key={item.id}>
                <td>{formatDate(item.created_at)}</td>
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
        <h2>Status transitions</h2>
        <table>
          <thead>
            <tr>
              <th>When</th>
              <th>From → to</th>
              <th>Who</th>
            </tr>
          </thead>
          <tbody>
            {transitions.map((item) => (
              <tr key={item.id}>
                <td>{formatDate(item.moved_at)}</td>
                <td>
                  {item.from_status_name || "Created"} → {item.to_status_name}
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
