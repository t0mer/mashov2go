-- Allow the OpenWA connector type. SQLite cannot change a CHECK constraint, so
-- connectors is rebuilt. Member references are cleared while the old table is
-- dropped and restored afterwards. Connector ids are kept, so sealed configs
-- still open: their AAD is connectors.config with the id.
CREATE TABLE connectors_new (
    id            INTEGER PRIMARY KEY,
    type          TEXT NOT NULL CHECK (type IN ('greenapi','telegram','openwa')),
    name          TEXT NOT NULL,
    config_sealed BLOB NOT NULL,
    is_default    INTEGER NOT NULL DEFAULT 0,
    enabled       INTEGER NOT NULL DEFAULT 1
);
INSERT INTO connectors_new(id, type, name, config_sealed, is_default, enabled)
    SELECT id, type, name, config_sealed, is_default, enabled FROM connectors;
CREATE TEMP TABLE member_connector AS
    SELECT id, whatsapp_connector_id FROM members WHERE whatsapp_connector_id IS NOT NULL;
UPDATE members SET whatsapp_connector_id = NULL WHERE whatsapp_connector_id IS NOT NULL;
DROP TABLE connectors;
ALTER TABLE connectors_new RENAME TO connectors;
UPDATE members SET whatsapp_connector_id = (
    SELECT mc.whatsapp_connector_id FROM member_connector mc WHERE mc.id = members.id)
  WHERE id IN (SELECT id FROM member_connector);
DROP TABLE member_connector;
