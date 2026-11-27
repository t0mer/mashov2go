package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Template is one stored override. Category is the syncer domain or plan category.
type Template struct {
	Category string
	Language string
	Audience string
	Body     string
}

// GetTemplate returns the stored override for a key. ok is false when none exists.
func (s *Store) GetTemplate(ctx context.Context, category, language, audience string) (Template, bool, error) {
	var t Template
	err := s.db.QueryRowContext(ctx,
		`SELECT category, language, audience, body FROM templates WHERE category = ? AND language = ? AND audience = ?`,
		category, language, audience).Scan(&t.Category, &t.Language, &t.Audience, &t.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, false, nil
	}
	if err != nil {
		return Template{}, false, err
	}
	return t, true, nil
}

// ListTemplates returns every stored override.
func (s *Store) ListTemplates(ctx context.Context) ([]Template, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, language, audience, body FROM templates ORDER BY category, language, audience`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		var t Template
		if err := rows.Scan(&t.Category, &t.Language, &t.Audience, &t.Body); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PutTemplate stores or replaces an override.
func (s *Store) PutTemplate(ctx context.Context, t Template, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO templates(category, language, audience, body, updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(category, language, audience) DO UPDATE SET body = excluded.body, updated_at = excluded.updated_at`,
		t.Category, t.Language, t.Audience, t.Body, now.UTC().Format(time.RFC3339))
	return err
}

// DeleteTemplate removes an override so the default applies again.
func (s *Store) DeleteTemplate(ctx context.Context, category, language, audience string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM templates WHERE category = ? AND language = ? AND audience = ?`, category, language, audience)
	return err
}
