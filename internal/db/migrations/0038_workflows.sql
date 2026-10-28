CREATE TABLE workflow (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    hop_budget INTEGER NOT NULL DEFAULT 30,
    graph TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
ALTER TABLE chat ADD COLUMN workflow_id TEXT REFERENCES workflow(id) ON DELETE SET NULL;
ALTER TABLE chat ADD COLUMN workflow_node TEXT;
ALTER TABLE chat ADD COLUMN workflow_run_id TEXT;
CREATE TABLE chat_handoff (
    id TEXT PRIMARY KEY,
    run_id TEXT,
    from_chat_id TEXT NOT NULL REFERENCES chat(id) ON DELETE CASCADE,
    to_chat_id TEXT NOT NULL REFERENCES chat(id) ON DELETE CASCADE,
    workflow_edge TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','ok','failed','stopped')),
    result TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    finished_at TEXT
);
CREATE INDEX chat_handoff_from_status ON chat_handoff(from_chat_id,status);
CREATE INDEX chat_handoff_to_status ON chat_handoff(to_chat_id,status);
CREATE TABLE workflow_join_input (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    edge_id TEXT NOT NULL,
    from_chat_id TEXT REFERENCES chat(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK(status IN ('ok','failed')),
    text TEXT NOT NULL,
    created_at TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    delivered INTEGER NOT NULL DEFAULT 0,
    UNIQUE(run_id,node_id,edge_id)
);
CREATE TABLE item_history_new (
    id TEXT PRIMARY KEY,
    item_type TEXT NOT NULL CHECK (item_type IN ('skill', 'connector', 'mcp_server', 'scheduled_task', 'bot', 'workflow')),
    item_id TEXT NOT NULL,
    item_name TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('created', 'updated', 'deleted', 'restored')),
    changes_json TEXT NOT NULL,
    snapshot_json TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES "user"(id),
    created_at TEXT NOT NULL
);
INSERT INTO item_history_new SELECT * FROM item_history;
DROP TABLE item_history;
ALTER TABLE item_history_new RENAME TO item_history;
CREATE INDEX item_history_item ON item_history(item_type, item_id, created_at);
