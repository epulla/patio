CREATE TABLE telegram_chat (
  telegram_chat_id INTEGER PRIMARY KEY,
  chat_id TEXT NOT NULL REFERENCES chat(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL
);

CREATE TABLE telegram_notice (
  item_id TEXT PRIMARY KEY,
  telegram_chat_id INTEGER NOT NULL,
  message_id INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
