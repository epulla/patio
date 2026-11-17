ALTER TABLE chat ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE scheduled_task ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE "user" ADD COLUMN effort TEXT NOT NULL DEFAULT '';

UPDATE chat SET effort='low' WHERE thinking=0;
UPDATE scheduled_task SET effort='low' WHERE thinking=0;
INSERT INTO app_setting(key,value)
SELECT 'effort', '"low"' WHERE EXISTS (
    SELECT 1 FROM app_setting WHERE key='thinking' AND value='false'
);
