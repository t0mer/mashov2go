-- One-time Telegram bind tokens (CLAUDE.md §8.1). Only the SHA-256 of the token
-- is stored; the plaintext lives in the deep link the member opens.
CREATE TABLE tg_link_tokens (
    token_hash TEXT PRIMARY KEY,
    member_id  INTEGER NOT NULL REFERENCES members(id),
    expires_at TEXT NOT NULL,
    used_at    TEXT
);
CREATE INDEX tg_link_tokens_member ON tg_link_tokens(member_id);
