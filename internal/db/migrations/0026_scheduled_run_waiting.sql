CREATE TABLE scheduled_task_run_new (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES scheduled_task(id) ON DELETE CASCADE,
    chat_id TEXT REFERENCES chat(id) ON DELETE SET NULL,
    scheduled_for TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running','waiting','done','failed','skipped')),
    late INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
INSERT INTO scheduled_task_run_new(id,task_id,chat_id,scheduled_for,status,late,error,created_at)
SELECT id,task_id,chat_id,scheduled_for,status,late,error,created_at FROM scheduled_task_run;
DROP TABLE scheduled_task_run;
ALTER TABLE scheduled_task_run_new RENAME TO scheduled_task_run;
CREATE INDEX scheduled_task_run_task_idx ON scheduled_task_run(task_id);
