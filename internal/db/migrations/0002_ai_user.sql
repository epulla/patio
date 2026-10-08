INSERT INTO "user" (id, name, email, email_verified, role)
VALUES ('ai', 'AI', 'ai@localhost', 1, 'user')
ON CONFLICT(id) DO NOTHING;
