export type Board = {
  id: string;
  name: string;
  short_name: string;
  template: string;
  created_at: string;
  updated_at: string;
};
export type Config = { show_hidden: boolean; version: string; chat_window: string; open_path: string };
export type ChatDefaults = {
  connector_id: string | null;
  model: string;
  folder: string | null;
  auto_allow: boolean;
  thinking: boolean;
  default_bot_id: string;
  show_thinking: boolean;
  language: "system" | "en" | "es";
};
export type SettingsAbout = {
  version: string;
  os: string;
  arch: string;
  db_path: string;
  log_path: string;
  data_dir: string;
  data_dir_bytes: number;
  approximate: boolean;
  counts: {
    boards: number;
    tickets: number;
    chats: number;
    connectors: number;
    mcp_servers: number;
    skills: number;
  };
  schema_version: number;
  show_hidden: boolean;
  secrets_store: string;
  repo_url: string;
};
export type SettingsRevealTarget = "database" | "logs" | "data_dir";
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
export type SearchTicket = Ticket & { board_name: string; status_name: string };
export type LinkType = "blocks" | "relates" | "duplicates";
export type TicketLink = {
  id: string;
  type: LinkType;
  direction: "outgoing" | "incoming";
  label: string;
  other_ticket_id: string;
  other_code: string;
  other_title: string;
  board_id: string;
  status: string;
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
export type CalendarScheduled = {
  task_id: string;
  title: string;
  at: string;
  run_id: string | null;
  chat_id: string | null;
  status: ScheduledTaskRun["status"] | null;
  late: boolean;
};
export type CalendarResponse = { items: CalendarItem[]; tickets: Ticket[]; scheduled: CalendarScheduled[] };
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

export type ConnectorKind = "claude" | "codex" | "opencode" | "copilot" | "local";
export type Connector = {
  id: string;
  kind: ConnectorKind;
  name: string;
  config_dir: string | null;
  base_url: string;
  model: string;
  created_at: string;
  has_secret: boolean;
};
export type ConnectorStatus = {
  kind: ConnectorKind;
  installed: boolean | null;
  path: string;
  version: string;
  logged_in: boolean | null;
  account: string;
  plan: string;
  note: string;
  install: { command: string; url: string } | null;
};
export type ConnectorQuota = {
  kind: ConnectorKind;
  windows: { label: string; used_percent: number | null; resets_at: string | null }[] | null;
  requests: number | null;
  tokens: { input: number; output: number; cost: number } | null;
  note: string;
  updated_at: string | null;
};

export type AllowedFolder = { id: string; path: string; created_at: string };
export type Chat = {
  id: string;
  connector_id: string | null;
  title: string;
  model: string;
  folder: string | null;
  session_id: string | null;
  created_at: string;
  updated_at: string;
  workdir: string;
  running: boolean;
  pending_count?: number;
  other_window?: boolean;
  thinking: boolean;
  show_thinking: boolean;
  auto_allow: boolean;
  capabilities: { thinking: { supported: boolean; reason: string }; steer: boolean };
  scheduled_task_id: string | null;
  scheduled_late: boolean;
  bot_id: string | null;
};
export type Bot = {
  id: string;
  name: string;
  description: string;
  persona_prompt: string;
  color: string;
  connector_id: string | null;
  model: string;
};
export type ScheduledTask = {
  id: string;
  title: string;
  prompt: string;
  connector_id: string | null;
  bot_id: string | null;
  model: string;
  folder: string | null;
  thinking: boolean;
  auto_allow: boolean;
  schedule: string;
  run_at: string | null;
  enabled: boolean;
  next_run_at: string | null;
  created_at: string;
  updated_at: string;
};
export type ScheduledTaskRun = {
  id: string;
  task_id: string;
  chat_id: string | null;
  scheduled_for: string;
  status: "running" | "waiting" | "done" | "failed" | "skipped";
  late: boolean;
  error: string;
  created_at: string;
};
export type Monitor = {
  id: string;
  name: string;
  mcp_server_id: string;
  tool: string;
  args: string;
  schedule: string;
  next_run_at: string | null;
  condition: "changed" | "non_empty" | "new_items";
  result_path: string;
  id_path: string;
  actions: string;
  enabled: boolean;
  last_run_at: string | null;
  last_status: "ok" | "changed" | "failed" | null;
  last_error: string;
  fail_streak: number;
  created_at: string;
  updated_at: string;
};
export type MonitorRun = {
  id: string;
  monitor_id: string;
  started_at: string;
  status: "ok" | "changed" | "failed";
  new_count: number;
  error: string;
};
export type ChatAttachment = { name: string; kind: "text" | "image" | "file"; mime: string; size: number };
export type ChatMessage = {
  id: string;
  chat_id: string;
  role: "user" | "assistant" | "tool" | "error" | "thinking" | "question";
  content: string;
  created_at: string;
  attachments: ChatAttachment[];
  answered: boolean;
  tool_result_size?: number;
  tool_error?: boolean;
  from_chat_id?: string | null;
  from_chat_title?: string;
};
export type ChatQuestion = {
  header: string;
  question: string;
  options: { label: string; description?: string }[];
  multiple?: boolean;
};
export type ChatAnswer = { header: string; selected: string[]; other?: string };
export type ChatApproval = {
  id: string;
  chat_id: string;
  tool_name: string;
  input_json: string;
  status: "pending" | "allowed" | "denied" | "expired";
  auto?: boolean;
  created_at: string;
  answered_at: string | null;
};

export type McpServer = {
  id: string;
  name: string;
  kind: "stdio" | "http";
  command: string;
  args: string[];
  env_keys: string[];
  url: string;
  auth: "none" | "bearer" | "oauth";
  enabled: boolean;
  has_bearer_token: boolean;
  has_oauth_token: boolean;
  oauth_client_id: string;
  oauth_scopes: string[];
  has_oauth_client_secret: boolean;
  created_at: string;
};
export type McpServerCheck = {
  ok: boolean;
  latency_ms: number;
  tools: { name: string; description: string }[];
  error: string;
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
    let message = lang() === "es" ? `Solicitud fallida (${response.status})` : `Request failed (${response.status})`;
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
  patch: <T>(path: string, value: unknown) =>
    request<T>(path, { method: "PATCH", body: JSON.stringify(value) }),
  delete: (path: string) => request<void>(path, { method: "DELETE" }),
  // WebKit drops File and Blob bodies sent to the app's custom scheme; an ArrayBuffer arrives intact.
  upload: async <T>(path: string, file: File) =>
    request<T>(`${path}?name=${encodeURIComponent(file.name)}`, {
      method: "POST",
      headers: { "Content-Type": "application/octet-stream" },
      body: await file.arrayBuffer(),
    }),
};
import { lang } from "./i18n";
