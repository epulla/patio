ALTER TABLE "user" ADD COLUMN persona_prompt TEXT NOT NULL DEFAULT '';
ALTER TABLE "user" ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE "user" ADD COLUMN color TEXT NOT NULL DEFAULT '';
ALTER TABLE "user" ADD COLUMN connector_id TEXT REFERENCES connector(id) ON DELETE SET NULL;
ALTER TABLE "user" ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE chat ADD COLUMN bot_id TEXT REFERENCES "user"(id) ON DELETE SET NULL;
ALTER TABLE scheduled_task ADD COLUMN bot_id TEXT REFERENCES "user"(id) ON DELETE SET NULL;
CREATE UNIQUE INDEX bot_name_unique ON "user"(name COLLATE NOCASE) WHERE is_ai=1 AND deleted_at IS NULL;
UPDATE "user" SET persona_prompt='You are Martin, playful, joyful AI companion for everyday/weekly tasks. Be supportive, concise, joke sometimes, recommend.', description='General assistant: plans and triages' WHERE id='ai';
INSERT INTO app_setting(key,value) SELECT 'default_bot_id','"none"' WHERE EXISTS (SELECT 1 FROM app_setting WHERE key='martin_persona' AND value='false') ON CONFLICT(key) DO NOTHING;
INSERT INTO app_setting(key,value) VALUES('default_bot_id','"ai"') ON CONFLICT(key) DO NOTHING;
DELETE FROM app_setting WHERE key='martin_persona';
CREATE TABLE item_history_new (
    id TEXT PRIMARY KEY,
    item_type TEXT NOT NULL CHECK (item_type IN ('skill', 'connector', 'mcp_server', 'scheduled_task', 'bot')),
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
