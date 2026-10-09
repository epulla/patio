CREATE TABLE allowed_folder (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);
CREATE TABLE chat (
    id TEXT PRIMARY KEY,
    connector_id TEXT REFERENCES connector(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    folder TEXT,
    session_id TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX chat_updated_idx ON chat(updated_at);
CREATE TABLE chat_message (
    id TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL REFERENCES chat(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool', 'error')),
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX chat_message_chat_idx ON chat_message(chat_id);
CREATE TABLE chat_approval (
    id TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL REFERENCES chat(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL,
    input_json TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'allowed', 'denied', 'expired')),
    created_at TEXT NOT NULL,
    answered_at TEXT
);
CREATE INDEX chat_approval_chat_idx ON chat_approval(chat_id);
