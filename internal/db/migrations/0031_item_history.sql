CREATE TABLE item_history (
    id TEXT PRIMARY KEY,
    item_type TEXT NOT NULL CHECK (item_type IN ('skill', 'connector', 'mcp_server', 'scheduled_task')),
    item_id TEXT NOT NULL,
    item_name TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('created', 'updated', 'deleted', 'restored')),
    changes_json TEXT NOT NULL,
    snapshot_json TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES "user"(id),
    created_at TEXT NOT NULL
);
CREATE INDEX item_history_item ON item_history(item_type, item_id, created_at);
