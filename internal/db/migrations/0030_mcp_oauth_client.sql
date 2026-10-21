ALTER TABLE mcp_server ADD COLUMN oauth_client_id TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_server ADD COLUMN oauth_scopes TEXT NOT NULL DEFAULT '[]';
