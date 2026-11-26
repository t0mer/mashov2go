package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// GetSetting reads one setting. found is false when the key is unset.
func (s *Store) GetSetting(ctx context.Context, key string, into any) (found bool, err error) {
	var raw string
	err = s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), into)
}

// SetSetting writes one setting as JSON.
func (s *Store) SetSetting(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, string(b))
	return err
}
