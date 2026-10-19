CREATE TABLE ticket_link (
    id TEXT PRIMARY KEY,
    from_ticket_id TEXT NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
    to_ticket_id TEXT NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('blocks','relates','duplicates')),
    created_at TEXT NOT NULL,
    CHECK (from_ticket_id <> to_ticket_id),
    UNIQUE (from_ticket_id, to_ticket_id, type)
);
