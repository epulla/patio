CREATE TABLE chat_draft (
    chat_id TEXT PRIMARY KEY REFERENCES chat(id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    attachments TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL
);
