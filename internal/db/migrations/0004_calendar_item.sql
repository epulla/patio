CREATE TABLE calendar_item (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('event', 'reminder')),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    starts_at TEXT NOT NULL,
    ends_at TEXT,
    all_day INTEGER NOT NULL DEFAULT 0,
    board_id TEXT REFERENCES board(id),
    ticket_id TEXT REFERENCES ticket(id) ON DELETE CASCADE,
    created_by TEXT NOT NULL REFERENCES "user"(id),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deleted_at TEXT,
    notified_at TEXT
);
CREATE INDEX calendar_item_starts_at_idx ON calendar_item (starts_at);
