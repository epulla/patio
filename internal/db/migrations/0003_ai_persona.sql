ALTER TABLE "user" ADD COLUMN is_ai INTEGER NOT NULL DEFAULT 0;
UPDATE "user"
SET name = 'Martin', last_name = NULL, email = 'martin-ai@localhost', is_ai = 1
WHERE id = 'ai';
