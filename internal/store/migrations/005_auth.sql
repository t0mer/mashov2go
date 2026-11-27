-- Browser sessions for the admin UI. Only the SHA-256 of the session token is stored.
CREATE TABLE ui_sessions (
    token_hash TEXT PRIMARY KEY,
    csrf       TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

-- Bearer API tokens. Only the SHA-256 of each token is stored.
CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    scope        TEXT NOT NULL CHECK (scope IN ('read','admin')),
    created_at   TEXT NOT NULL,
    last_used_at TEXT,
    revoked_at   TEXT
);
