# Patio database schema

```mermaid
erDiagram
  user { TEXT id; TEXT name; TEXT last_name; TEXT email; INTEGER email_verified; TEXT id_card; TEXT phone; TEXT image; TEXT role; TEXT created_at; TEXT updated_at; TEXT deleted_at; INTEGER is_ai }
  board { TEXT id; TEXT name; TEXT short_name; TEXT template; TEXT created_at; TEXT updated_at; TEXT deleted_at; INTEGER position }
  status { TEXT id; TEXT name; TEXT type; TEXT additional_fields; TEXT created_at; TEXT updated_at; TEXT deleted_at; TEXT board_id }
  ticket_type { TEXT id; TEXT name; TEXT color; TEXT created_at; TEXT updated_at; TEXT deleted_at; TEXT board_id }
  ticket { TEXT id; TEXT title; TEXT description; INTEGER number; TEXT code; TEXT additional_fields; TEXT created_at; TEXT updated_at; TEXT due_date; TEXT status_updated_at; TEXT status_id; TEXT board_id; TEXT ticket_type_id; TEXT reporter_id; TEXT assignee_id }
  ticket_link { TEXT id; TEXT from_ticket_id; TEXT to_ticket_id; TEXT type; TEXT created_at }
  ticket_history { TEXT id; TEXT content; TEXT created_at; TEXT ticket_id; TEXT user_id }
  ticket_comment { TEXT id; TEXT content; TEXT created_at; TEXT updated_at; TEXT ticket_id; TEXT user_id }
  status_order { TEXT id; TEXT status_ids; TEXT updated_at; TEXT board_id }
  ticket_order { TEXT id; TEXT ticket_ids; TEXT updated_at; TEXT status_id }
  ticket_type_order { TEXT id; TEXT ticket_type_ids; TEXT updated_at; TEXT board_id }
  board_counter { INTEGER id; INTEGER last_number; TEXT board_id }
  old_ticket_code { INTEGER id; TEXT old_ticket_code; TEXT created_at; TEXT ticket_id }
  ticket_status_transition { TEXT id; TEXT ticket_id; TEXT board_id; TEXT from_status_id; TEXT to_status_id; TEXT user_id; TEXT moved_at }
  calendar_item { TEXT id; TEXT kind; TEXT title; TEXT description; TEXT starts_at; TEXT ends_at; INTEGER all_day; TEXT board_id; TEXT ticket_id; TEXT created_by; TEXT created_at; TEXT updated_at; TEXT deleted_at; TEXT notified_at }
  connector { TEXT id; TEXT kind; TEXT name; TEXT config_dir; TEXT base_url; TEXT model; TEXT quota_json; TEXT created_at }
  allowed_folder { TEXT id; TEXT path; TEXT created_at }
  chat { TEXT id; TEXT connector_id; TEXT title; TEXT model; TEXT folder; TEXT session_id; TEXT created_at; TEXT updated_at; INTEGER thinking; INTEGER show_thinking; INTEGER auto_allow; TEXT scheduled_task_id }
  chat_message { TEXT id; TEXT chat_id; TEXT role; TEXT content; TEXT created_at; TEXT tool_result; TEXT attachments; INTEGER answered }
  chat_approval { TEXT id; TEXT chat_id; TEXT tool_name; TEXT input_json; TEXT status; TEXT created_at; TEXT answered_at; INTEGER auto }
  mcp_server { TEXT id; TEXT name; TEXT kind; TEXT command; TEXT args_json; TEXT env_keys_json; TEXT url; TEXT auth; INTEGER enabled; TEXT created_at }
  skill { TEXT id; TEXT name; TEXT description; TEXT body; INTEGER enabled; TEXT created_at; TEXT updated_at; TEXT kind }
  app_setting { TEXT key; TEXT value }
  scheduled_task { TEXT id; TEXT title; TEXT prompt; TEXT connector_id; TEXT model; TEXT folder; INTEGER thinking; INTEGER auto_allow; TEXT schedule; TEXT run_at; INTEGER enabled; TEXT next_run_at; TEXT created_at; TEXT updated_at }
  scheduled_task_run { TEXT id; TEXT task_id; TEXT chat_id; TEXT scheduled_for; TEXT status; INTEGER late; TEXT error; TEXT created_at }
  app_window { TEXT id; TEXT chat_id; INTEGER pid; TEXT heartbeat_at; TEXT focus_at; TEXT focus_path }
  chat_turn { TEXT chat_id; TEXT window_id; TEXT started_at }
  notification { TEXT id; TEXT kind; TEXT title; TEXT body; TEXT link; TEXT created_at; TEXT read_at }
```

Ordering is stored in `board.position` and JSON arrays: `status_order`, `ticket_order`, `ticket_type_order`. Ticket codes are `<short_name>-<number>`, allocated by `board_counter`. Users with `is_ai=1` represent Martin, the AI. IDs are UUIDv4 TEXT; timestamps are ISO-8601 UTC TEXT.
Links use `blocks`, `relates`, or `duplicates`; each link has one direction and appears inversely from the other ticket. Links cascade-delete with either ticket.
Scheduled run statuses are `running`, `waiting`, `done`, `failed`, or `skipped`.
