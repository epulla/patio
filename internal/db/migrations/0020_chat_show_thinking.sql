ALTER TABLE chat ADD COLUMN show_thinking INTEGER NOT NULL DEFAULT 0 CHECK (show_thinking IN (0, 1));

CREATE TABLE chat_message_new (
    id TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL REFERENCES chat(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool', 'error', 'thinking')),
    content TEXT NOT NULL,
    created_at TEXT NOT NULL,
    tool_result TEXT,
    attachments TEXT NOT NULL DEFAULT '[]'
);
INSERT INTO chat_message_new(id,chat_id,role,content,created_at,tool_result,attachments)
SELECT id,chat_id,role,content,created_at,tool_result,attachments FROM chat_message;
DROP TABLE chat_message;
ALTER TABLE chat_message_new RENAME TO chat_message;
CREATE INDEX chat_message_chat_idx ON chat_message(chat_id);
