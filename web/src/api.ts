export type Board = {
  id: string;
  name: string;
  short_name: string;
  template: string;
  created_at: string;
  updated_at: string;
};
export type Config = { show_hidden: boolean; version: string };
export type Status = {
  id: string;
  name: string;
  type: "classic" | "temporary";
  board_id: string;
  hide_after_days: number;
  tickets: Ticket[];
};
export type TicketType = {
  id: string;
  name: string;
  color: string;
  board_id: string;
};
export type Ticket = {
  id: string;
  title: string;
  description: string;
  number: number;
  code: string;
  created_at: string;
  updated_at: string;
  due_date: string | null;
  status_updated_at: string | null;
  status_id: string;
  board_id: string;
  ticket_type_id: string | null;
  reporter_id: string | null;
  assignee_id: string | null;
};
export type CalendarItem = {
  id: string;
  kind: "event" | "reminder";
  title: string;
  description: string;
  starts_at: string;
  ends_at: string | null;
  all_day: boolean;
  board_id: string | null;
  ticket_id: string | null;
  created_by: string;
  created_by_is_ai: boolean;
};
export type CalendarResponse = { items: CalendarItem[]; tickets: Ticket[] };
export type McpStatus = {
  command: string;
  args: string[];
  ok: boolean;
  error: string;
  tools: { name: string; description: string }[];
  latency_ms: number;
};
export type BoardData = {
  board: Board;
  statuses: Status[];
  ticket_types: TicketType[];
};
export type User = {
  id: string;
  name: string;
  last_name: string | null;
  email: string;
  is_ai: boolean;
};
export type History = {
  id: string;
  content: string;
  created_at: string;
  user_id: string;
  user_name: string;
  user_is_ai: boolean;
};
export type Transition = {
  id: string;
  from_status_id: string | null;
  from_status_name: string | null;
  to_status_id: string;
  to_status_name: string;
  user_id: string;
  user_name: string;
  user_is_ai: boolean;
  moved_at: string;
};
export type Comment = {
  id: string;
  content: string;
  created_at: string;
  updated_at: string;
  user_id: string;
  user_name: string;
  user_is_ai: boolean;
};

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: {
      "Content-Type": "application/json",
      ...(options?.headers || {}),
    },
    ...options,
  });
  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    try {
      message = (await response.json()).error || message;
    } catch {}
    throw new Error(message);
  }
  if (response.status === 204) return undefined as T;
  return response.json();
}
const body = (value: unknown): RequestInit => ({
  method: "POST",
  body: JSON.stringify(value),
});
export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, value: unknown) => request<T>(path, body(value)),
  put: <T>(path: string, value: unknown) =>
    request<T>(path, { method: "PUT", body: JSON.stringify(value) }),
  delete: (path: string) => request<void>(path, { method: "DELETE" }),
};
