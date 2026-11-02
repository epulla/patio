CREATE TABLE ticket_attachment (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    original_name TEXT NOT NULL,
    mime TEXT NOT NULL,
    kind TEXT NOT NULL,
    size INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(ticket_id, name)
);
CREATE INDEX ticket_attachment_ticket ON ticket_attachment(ticket_id);
