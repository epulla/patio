ALTER TABLE chat ADD COLUMN title_locked INTEGER NOT NULL DEFAULT 0 CHECK (title_locked IN (0, 1));
