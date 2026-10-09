CREATE TABLE mcp_server (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL CHECK (kind IN ('stdio', 'http')),
    command TEXT NOT NULL DEFAULT '',
    args_json TEXT NOT NULL,
    env_keys_json TEXT NOT NULL,
    url TEXT NOT NULL DEFAULT '',
    auth TEXT NOT NULL CHECK (auth IN ('none', 'bearer', 'oauth')),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at TEXT NOT NULL
);
