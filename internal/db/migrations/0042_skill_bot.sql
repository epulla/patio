CREATE TABLE skill_bot (
    skill_id TEXT NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    bot_id TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    PRIMARY KEY(skill_id, bot_id)
);
CREATE INDEX skill_bot_bot ON skill_bot(bot_id);
