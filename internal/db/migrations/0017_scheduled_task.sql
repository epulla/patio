CREATE TABLE scheduled_task (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    prompt TEXT NOT NULL,
    connector_id TEXT REFERENCES connector(id) ON DELETE SET NULL,
    model TEXT NOT NULL DEFAULT '',
    folder TEXT,
    thinking INTEGER NOT NULL DEFAULT 0,
    auto_allow INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL DEFAULT '',
    run_at TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    next_run_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK ((schedule <> '' AND run_at IS NULL) OR (schedule = '' AND run_at IS NOT NULL))
);
CREATE TABLE scheduled_task_run (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES scheduled_task(id) ON DELETE CASCADE,
    chat_id TEXT REFERENCES chat(id) ON DELETE SET NULL,
    scheduled_for TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running','done','failed','skipped')),
    late INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX scheduled_task_run_task_idx ON scheduled_task_run(task_id);
ALTER TABLE chat ADD COLUMN scheduled_task_id TEXT REFERENCES scheduled_task(id) ON DELETE SET NULL;
