CREATE TABLE notification (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    link TEXT NOT NULL,
    created_at TEXT NOT NULL,
    read_at TEXT
);
CREATE INDEX notification_created_at_idx ON notification(created_at DESC);
