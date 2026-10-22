CREATE TABLE monitor (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    mcp_server_id TEXT NOT NULL REFERENCES mcp_server(id) ON DELETE CASCADE,
    tool TEXT NOT NULL,
    args TEXT NOT NULL DEFAULT '{}',
    schedule TEXT NOT NULL,
    next_run_at TEXT,
    condition TEXT NOT NULL DEFAULT 'changed' CHECK (condition IN ('changed','non_empty','new_items')),
    result_path TEXT NOT NULL DEFAULT '',
    id_path TEXT NOT NULL DEFAULT '',
    actions TEXT NOT NULL DEFAULT '{}',
    enabled INTEGER NOT NULL DEFAULT 1,
    last_run_at TEXT,
    last_status TEXT CHECK (last_status IS NULL OR last_status IN ('ok','changed','failed')),
    last_error TEXT NOT NULL DEFAULT '',
    last_hash TEXT NOT NULL DEFAULT '',
    fail_streak INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE monitor_run (
    id TEXT PRIMARY KEY,
    monitor_id TEXT NOT NULL REFERENCES monitor(id) ON DELETE CASCADE,
    started_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('ok','changed','failed')),
    result_hash TEXT NOT NULL,
    new_count INTEGER NOT NULL,
    error TEXT NOT NULL
);
CREATE INDEX monitor_run_monitor_started ON monitor_run(monitor_id, started_at);
CREATE TABLE monitor_seen (
    monitor_id TEXT NOT NULL REFERENCES monitor(id) ON DELETE CASCADE,
    item_id TEXT NOT NULL,
    PRIMARY KEY (monitor_id,item_id)
);
