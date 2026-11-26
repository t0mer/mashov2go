package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// ErrConnectorInUse is returned by DeleteConnector while members still use the connector.
var ErrConnectorInUse = errors.New("connector is in use by members")

// Connector is one provider account. Config (the provider JSON, including secrets)
// is never part of a listing; read it only with ConnectorConfig.
type Connector struct {
	ID        int64
	Type      string // "greenapi" | "telegram"
	Name      string
	IsDefault bool
	Enabled   bool
}

func connectorAAD(id int64) []byte {
	return []byte("connectors.config:" + strconv.FormatInt(id, 10))
}

// CreateConnector stores a connector with its config sealed. The row is inserted
// first so the AAD can bind the sealed value to the row id.
func (s *Store) CreateConnector(ctx context.Context, c Connector, config []byte) (Connector, error) {
	if c.Name == "" {
		return c, errors.New("connector name is required")
	}
	if len(config) == 0 {
		return c, errors.New("connector config is required")
	}
	if c.IsDefault && c.Type == "telegram" {
		return c, errors.New("a telegram connector cannot be the default WhatsApp connector")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO connectors(type, name, config_sealed, is_default, enabled) VALUES(?,?,?,?,?)`,
		c.Type, c.Name, []byte{}, boolInt(c.IsDefault), boolInt(c.Enabled))
	if err != nil {
		return c, fmt.Errorf("inserting connector: %w", err)
	}
	if c.ID, err = res.LastInsertId(); err != nil {
		return c, err
	}
	sealed, err := s.sealer.Seal(config, connectorAAD(c.ID))
	if err != nil {
		return c, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE connectors SET config_sealed = ? WHERE id = ?`, sealed, c.ID); err != nil {
		return c, err
	}
	if c.IsDefault {
		if err := clearDefaultTx(ctx, tx, c.ID); err != nil {
			return c, err
		}
	}
	return c, tx.Commit()
}

// ListConnectors returns connectors without their secrets, oldest first.
func (s *Store) ListConnectors(ctx context.Context) ([]Connector, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, name, is_default, enabled FROM connectors ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connector
	for rows.Next() {
		var c Connector
		var def, en int
		if err := rows.Scan(&c.ID, &c.Type, &c.Name, &def, &en); err != nil {
			return nil, err
		}
		c.IsDefault, c.Enabled = def == 1, en == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// ConnectorConfig opens and returns the sealed provider config of one connector.
// Callers must not log or return it to the API.
func (s *Store) ConnectorConfig(ctx context.Context, id int64) ([]byte, error) {
	var sealed []byte
	err := s.db.QueryRowContext(ctx, `SELECT config_sealed FROM connectors WHERE id = ?`, id).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	pt, err := s.sealer.Open(sealed, connectorAAD(id))
	if err != nil {
		return nil, fmt.Errorf("opening connector %d config: %w", id, err)
	}
	return pt, nil
}

// UpdateConnector changes name and enabled state. A nil config keeps the stored secret.
func (s *Store) UpdateConnector(ctx context.Context, id int64, name string, enabled bool, config []byte) error {
	if name == "" {
		return errors.New("connector name is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		`UPDATE connectors SET name = ?, enabled = ? WHERE id = ?`, name, boolInt(enabled), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if config != nil {
		sealed, err := s.sealer.Seal(config, connectorAAD(id))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE connectors SET config_sealed = ? WHERE id = ?`, sealed, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetDefaultConnector makes id the default WhatsApp connector. Telegram
// connectors cannot be default.
func (s *Store) SetDefaultConnector(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var typ string
	if err := tx.QueryRowContext(ctx, `SELECT type FROM connectors WHERE id = ?`, id).Scan(&typ); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if typ == "telegram" {
		return errors.New("a telegram connector cannot be the default WhatsApp connector")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE connectors SET is_default = 0 WHERE type <> 'telegram'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE connectors SET is_default = 1 WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteConnector removes a connector. It refuses while any member references it,
// so the caller must reassign those members first.
func (s *Store) DeleteConnector(ctx context.Context, id int64) error {
	var used int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM members WHERE whatsapp_connector_id = ?`, id).Scan(&used); err != nil {
		return err
	}
	if used > 0 {
		return ErrConnectorInUse
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM connectors WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func clearDefaultTx(ctx context.Context, tx *sql.Tx, keep int64) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE connectors SET is_default = 0 WHERE id <> ? AND type <> 'telegram'`, keep)
	return err
}
