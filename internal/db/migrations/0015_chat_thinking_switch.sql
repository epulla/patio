ALTER TABLE chat DROP COLUMN tools;
DELETE FROM app_setting WHERE key='tools';
UPDATE chat SET thinking=1 WHERE connector_id IN (SELECT id FROM connector WHERE kind<>'local');
