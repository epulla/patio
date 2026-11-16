ALTER TABLE chat ADD COLUMN effort TEXT NOT NULL DEFAULT 'default';
ALTER TABLE scheduled_task ADD COLUMN effort TEXT NOT NULL DEFAULT 'default';
ALTER TABLE "user" ADD COLUMN effort TEXT NOT NULL DEFAULT 'default';

UPDATE chat SET effort='small' WHERE thinking=0;
UPDATE scheduled_task SET effort='small' WHERE thinking=0;
INSERT INTO app_setting(key,value)
SELECT 'effort', '"small"' WHERE EXISTS (
    SELECT 1 FROM app_setting WHERE key='thinking' AND value='false'
);
