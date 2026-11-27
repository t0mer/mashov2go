-- Message template overrides (CLAUDE.md §7, §9). A missing row means the built-in
-- default applies. audience is 'child' or 'parent'; language is 'he' or 'en'.
CREATE TABLE templates (
    category   TEXT NOT NULL,
    language   TEXT NOT NULL CHECK (language IN ('he','en')),
    audience   TEXT NOT NULL CHECK (audience IN ('child','parent')),
    body       TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (category, language, audience)
);
