ALTER TABLE skill ADD COLUMN kind TEXT NOT NULL DEFAULT 'skill' CHECK (kind IN ('skill', 'memory'));
