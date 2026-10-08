CREATE TABLE IF NOT EXISTS "user" (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    last_name TEXT,
    email TEXT NOT NULL UNIQUE,
    email_verified INTEGER NOT NULL DEFAULT 0,
    id_card TEXT,
    phone TEXT,
    image TEXT,
    role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deleted_at TEXT
);
CREATE INDEX IF NOT EXISTS user_name_idx ON "user" (name);
CREATE INDEX IF NOT EXISTS user_last_name_idx ON "user" (last_name);
CREATE INDEX IF NOT EXISTS user_email_idx ON "user" (email);
CREATE INDEX IF NOT EXISTS user_id_card_idx ON "user" (id_card);

CREATE TABLE IF NOT EXISTS board (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    short_name TEXT NOT NULL UNIQUE CHECK (length(short_name) <= 10),
    template TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deleted_at TEXT
);
CREATE INDEX IF NOT EXISTS board_short_name_idx ON board (short_name);
CREATE INDEX IF NOT EXISTS board_created_at_idx ON board (created_at);
CREATE INDEX IF NOT EXISTS board_updated_at_idx ON board (updated_at);

CREATE TABLE IF NOT EXISTS status (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('classic', 'temporary')),
    additional_fields TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deleted_at TEXT,
    board_id TEXT NOT NULL REFERENCES board(id)
);
CREATE INDEX IF NOT EXISTS status_created_at_idx ON status (created_at);
CREATE INDEX IF NOT EXISTS status_updated_at_idx ON status (updated_at);

CREATE TABLE IF NOT EXISTS ticket_type (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deleted_at TEXT,
    board_id TEXT NOT NULL REFERENCES board(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS ticket (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    number INTEGER,
    code TEXT UNIQUE,
    additional_fields TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    due_date TEXT,
    status_updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    status_id TEXT NOT NULL REFERENCES status(id),
    board_id TEXT NOT NULL REFERENCES board(id),
    ticket_type_id TEXT REFERENCES ticket_type(id),
    reporter_id TEXT NOT NULL REFERENCES "user"(id),
    assignee_id TEXT REFERENCES "user"(id),
    UNIQUE (board_id, number)
);
CREATE INDEX IF NOT EXISTS ticket_title_idx ON ticket (title);
CREATE INDEX IF NOT EXISTS ticket_number_idx ON ticket (number);
CREATE INDEX IF NOT EXISTS ticket_created_at_idx ON ticket (created_at);
CREATE INDEX IF NOT EXISTS ticket_updated_at_idx ON ticket (updated_at);
CREATE INDEX IF NOT EXISTS ticket_status_updated_at_idx ON ticket (status_updated_at);

CREATE TABLE IF NOT EXISTS ticket_history (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ticket_id TEXT NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES "user"(id)
);
CREATE INDEX IF NOT EXISTS ticket_history_created_at_idx ON ticket_history (created_at);

CREATE TABLE IF NOT EXISTS ticket_comment (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ticket_id TEXT NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES "user"(id)
);
CREATE INDEX IF NOT EXISTS ticket_comment_created_at_idx ON ticket_comment (created_at);
CREATE INDEX IF NOT EXISTS ticket_comment_updated_at_idx ON ticket_comment (updated_at);

CREATE TABLE IF NOT EXISTS status_order (
    id TEXT PRIMARY KEY,
    status_ids TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    board_id TEXT NOT NULL UNIQUE REFERENCES board(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS ticket_order (
    id TEXT PRIMARY KEY,
    ticket_ids TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    status_id TEXT NOT NULL UNIQUE REFERENCES status(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS ticket_type_order (
    id TEXT PRIMARY KEY,
    ticket_type_ids TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    board_id TEXT NOT NULL UNIQUE REFERENCES board(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS board_counter (
    id INTEGER PRIMARY KEY,
    last_number INTEGER NOT NULL DEFAULT 0,
    board_id TEXT NOT NULL UNIQUE REFERENCES board(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS old_ticket_code (
    id INTEGER PRIMARY KEY,
    old_ticket_code TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ticket_id TEXT NOT NULL REFERENCES ticket(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS ticket_status_transition (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
    board_id TEXT NOT NULL REFERENCES board(id),
    from_status_id TEXT REFERENCES status(id),
    to_status_id TEXT NOT NULL REFERENCES status(id),
    user_id TEXT NOT NULL REFERENCES "user"(id),
    moved_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS ticket_status_transition_board_moved_idx ON ticket_status_transition (board_id, moved_at);
CREATE INDEX IF NOT EXISTS ticket_status_transition_ticket_moved_idx ON ticket_status_transition (ticket_id, moved_at);
