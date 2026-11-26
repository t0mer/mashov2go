CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL              -- JSON
);

CREATE TABLE accounts (
    id                INTEGER PRIMARY KEY,
    name              TEXT NOT NULL,
    semel             INTEGER NOT NULL,
    year              INTEGER NOT NULL,
    username          TEXT NOT NULL,
    password_sealed   BLOB,          -- AES-256-GCM, aad = accounts.password:<id>
    devicepass_sealed BLOB,          -- AES-256-GCM, aad = accounts.devicepass:<id>
    device_uuid       TEXT NOT NULL,
    auth_state        TEXT NOT NULL DEFAULT 'ok',
    created_at        TEXT NOT NULL
);

CREATE TABLE students (
    account_id   INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    child_guid   TEXT NOT NULL,
    private_name TEXT NOT NULL,
    family_name  TEXT NOT NULL,
    PRIMARY KEY (account_id, child_guid)
);

-- Every synced item of a (child, domain). Gone items are soft-deleted.
CREATE TABLE items (
    account_id   INTEGER NOT NULL,
    child_guid   TEXT NOT NULL,
    domain       TEXT NOT NULL,
    item_key     TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    payload      TEXT NOT NULL,      -- JSON
    first_seen   TEXT NOT NULL,
    last_seen    TEXT NOT NULL,
    deleted_at   TEXT,
    PRIMARY KEY (account_id, child_guid, domain, item_key)
);

-- Baseline state per (child, domain): the first sync stores silently (CLAUDE.md §0.6).
CREATE TABLE domain_state (
    account_id      INTEGER NOT NULL,
    child_guid      TEXT NOT NULL,
    domain          TEXT NOT NULL,
    baselined_at    TEXT NOT NULL,
    last_success_at TEXT NOT NULL,
    PRIMARY KEY (account_id, child_guid, domain)
);

CREATE TABLE events (
    id          INTEGER PRIMARY KEY,
    category    TEXT NOT NULL,
    kind        TEXT NOT NULL,
    child_guid  TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    detected_at TEXT NOT NULL,
    data        TEXT NOT NULL,       -- JSON
    source      TEXT NOT NULL,
    dedupe_key  TEXT NOT NULL UNIQUE -- child|domain|item_key|content_hash, see CLAUDE.md §6.4
);
CREATE INDEX events_detected ON events(detected_at);
