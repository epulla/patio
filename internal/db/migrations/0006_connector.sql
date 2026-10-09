CREATE TABLE connector (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('claude', 'codex', 'opencode', 'copilot', 'local')),
    name TEXT NOT NULL,
    config_dir TEXT,
    base_url TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    quota_json TEXT,
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX connector_existing_kind_idx ON connector(kind) WHERE config_dir IS NULL AND kind != 'local';
