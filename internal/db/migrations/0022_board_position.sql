ALTER TABLE board ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

UPDATE board
SET position = (
  SELECT COUNT(*)
  FROM board AS newer
  WHERE newer.created_at > board.created_at
     OR (newer.created_at = board.created_at AND newer.rowid < board.rowid)
);
