UPDATE chat SET effort='' WHERE effort='default';
UPDATE chat SET effort='low' WHERE effort='small';
UPDATE scheduled_task SET effort='' WHERE effort='default';
UPDATE scheduled_task SET effort='low' WHERE effort='small';
UPDATE "user" SET effort='' WHERE effort='default';
UPDATE "user" SET effort='low' WHERE effort='small';
UPDATE app_setting SET value='""' WHERE key='effort' AND value='"default"';
UPDATE app_setting SET value='"low"' WHERE key='effort' AND value='"small"';
