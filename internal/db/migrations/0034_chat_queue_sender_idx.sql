CREATE INDEX chat_queue_from_chat_idx ON chat_queue(from_chat_id) WHERE from_chat_id IS NOT NULL;
