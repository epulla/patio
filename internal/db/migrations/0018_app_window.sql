CREATE TABLE app_window (
    id TEXT PRIMARY KEY,
    chat_id TEXT,
    pid INTEGER NOT NULL,
    heartbeat_at TEXT NOT NULL,
    focus_at TEXT
);

CREATE TABLE chat_turn (
    chat_id TEXT PRIMARY KEY,
    window_id TEXT NOT NULL,
    started_at TEXT NOT NULL
);
