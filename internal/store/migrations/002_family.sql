CREATE TABLE members (
    id                      INTEGER PRIMARY KEY,
    role                    TEXT NOT NULL CHECK (role IN ('parent','child')),
    name                    TEXT NOT NULL,
    language                TEXT NOT NULL DEFAULT 'he' CHECK (language IN ('he','en')),
    whatsapp_phone          TEXT NOT NULL DEFAULT '',   -- E.164 digits, no '+'
    telegram_chat_id        INTEGER,
    channel                 TEXT NOT NULL DEFAULT 'whatsapp' CHECK (channel IN ('whatsapp','telegram')),
    whatsapp_connector_id   INTEGER REFERENCES connectors(id),
    fallback_channel        TEXT CHECK (fallback_channel IN ('whatsapp','telegram')),
    quiet_start             TEXT NOT NULL DEFAULT '21:00',
    quiet_end               TEXT NOT NULL DEFAULT '07:00',
    plan_enabled            INTEGER NOT NULL DEFAULT 1,
    plan_time               TEXT NOT NULL DEFAULT '19:30',
    include_homework_today  INTEGER NOT NULL DEFAULT 1,
    active                  INTEGER NOT NULL DEFAULT 1,
    created_at              TEXT NOT NULL,
    deleted_at              TEXT
);

-- A child member is linked to exactly one Mashov student.
CREATE TABLE member_links (
    member_id  INTEGER PRIMARY KEY REFERENCES members(id),
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    child_guid TEXT NOT NULL,
    UNIQUE (account_id, child_guid)
);

-- Who receives each category for a child: child | parents | both | off.
CREATE TABLE delivery_prefs (
    child_member_id INTEGER NOT NULL REFERENCES members(id),
    category        TEXT NOT NULL,
    recipient       TEXT NOT NULL CHECK (recipient IN ('child','parents','both','off')),
    PRIMARY KEY (child_member_id, category)
);

-- Per-parent mute for one child and category.
CREATE TABLE parent_mutes (
    parent_member_id INTEGER NOT NULL REFERENCES members(id),
    child_member_id  INTEGER NOT NULL REFERENCES members(id),
    category         TEXT NOT NULL,
    PRIMARY KEY (parent_member_id, child_member_id, category)
);

-- Outbound messages. One row per (event, member): never send the same event twice.
CREATE TABLE deliveries (
    id         INTEGER PRIMARY KEY,
    event_id   INTEGER NOT NULL REFERENCES events(id),
    member_id  INTEGER NOT NULL REFERENCES members(id),
    channel    TEXT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN ('pending','sent','failed','skipped_no_channel')),
    attempts   INTEGER NOT NULL DEFAULT 0,
    next_at    TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    UNIQUE (event_id, member_id)
);
CREATE INDEX deliveries_due ON deliveries(status, next_at);

-- Connectors. Config (credentials, URLs) is sealed; is_default marks the WhatsApp default.
CREATE TABLE connectors (
    id            INTEGER PRIMARY KEY,
    type          TEXT NOT NULL CHECK (type IN ('greenapi','telegram')),
    name          TEXT NOT NULL,
    config_sealed BLOB NOT NULL,   -- AES-256-GCM, aad = connectors.config:<id>
    is_default    INTEGER NOT NULL DEFAULT 0,
    enabled       INTEGER NOT NULL DEFAULT 1
);

-- Daily plan sent per member and date; the hash detects late changes (CLAUDE.md §6.2).
CREATE TABLE plans_sent (
    member_id    INTEGER NOT NULL REFERENCES members(id),
    plan_date    TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    sent_at      TEXT NOT NULL,
    PRIMARY KEY (member_id, plan_date)
);
