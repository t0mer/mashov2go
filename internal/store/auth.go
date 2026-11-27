package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const settingAdminHash = "auth.admin_password_hash"

// APIToken is a stored token without its secret. Scope is "read" or "admin".
type APIToken struct {
	ID         int64
	Name       string
	Scope      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// AdminPasswordHash returns the stored bcrypt hash, or "" when no admin password is set.
func (s *Store) AdminPasswordHash(ctx context.Context) (string, error) {
	var h string
	if _, err := s.GetSetting(ctx, settingAdminHash, &h); err != nil {
		return "", err
	}
	return h, nil
}

// SetAdminPasswordHashIfUnset stores the hash only when no admin password exists.
// It reports whether this call set it, so first-run setup can happen once.
func (s *Store) SetAdminPasswordHashIfUnset(ctx context.Context, hash string) (bool, error) {
	enc, err := json.Marshal(hash)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO settings(key, value) VALUES(?, ?)`, settingAdminHash, string(enc))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// CreateUISession stores a browser session. Only its token hash is kept.
func (s *Store) CreateUISession(ctx context.Context, tokenHash, csrf string, expiresAt, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ui_sessions(token_hash, csrf, expires_at, created_at) VALUES(?,?,?,?)`,
		tokenHash, csrf, expiresAt.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	return err
}

// UISession returns the CSRF token of a live session. ok is false when the
// session is unknown or expired.
func (s *Store) UISession(ctx context.Context, tokenHash string, now time.Time) (csrf string, ok bool, err error) {
	var expires string
	err = s.db.QueryRowContext(ctx,
		`SELECT csrf, expires_at FROM ui_sessions WHERE token_hash = ?`, tokenHash).Scan(&csrf, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || !now.Before(exp) {
		return "", false, nil
	}
	return csrf, true, nil
}

// DeleteUISession ends a browser session (logout).
func (s *Store) DeleteUISession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ui_sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// PurgeExpiredUISessions removes sessions that have expired.
func (s *Store) PurgeExpiredUISessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM ui_sessions WHERE expires_at <= ?`, now.UTC().Format(time.RFC3339))
	return err
}

// CreateAPIToken stores a new token and returns its metadata.
func (s *Store) CreateAPIToken(ctx context.Context, name, scope, tokenHash string, now time.Time) (APIToken, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens(name, token_hash, scope, created_at) VALUES(?,?,?,?)`,
		name, tokenHash, scope, now.UTC().Format(time.RFC3339))
	if err != nil {
		return APIToken{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return APIToken{}, err
	}
	return APIToken{ID: id, Name: name, Scope: scope, CreatedAt: now.UTC()}, nil
}

// ListAPITokens returns every token, revoked ones included, without secrets.
func (s *Store) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, scope, created_at, last_used_at, revoked_at FROM api_tokens ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeAPIToken disables a token. ErrNotFound when it does not exist or is already revoked.
func (s *Store) RevokeAPIToken(ctx context.Context, id int64, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ActiveAPIToken returns the live (not revoked) token with this hash and records its use.
// ok is false for an unknown or revoked token.
func (s *Store) ActiveAPIToken(ctx context.Context, tokenHash string, now time.Time) (APIToken, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, scope, created_at, last_used_at, revoked_at FROM api_tokens
		 WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash)
	t, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, false, nil
	}
	if err != nil {
		return APIToken{}, false, err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, now.UTC().Format(time.RFC3339), t.ID); err != nil {
		return APIToken{}, false, err
	}
	return t, true, nil
}

type rowScan interface {
	Scan(dest ...any) error
}

func scanAPIToken(r rowScan) (APIToken, error) {
	var t APIToken
	var created string
	var used, revoked sql.NullString
	if err := r.Scan(&t.ID, &t.Name, &t.Scope, &created, &used, &revoked); err != nil {
		return APIToken{}, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if used.Valid {
		v, _ := time.Parse(time.RFC3339, used.String)
		t.LastUsedAt = &v
	}
	if revoked.Valid {
		v, _ := time.Parse(time.RFC3339, revoked.String)
		t.RevokedAt = &v
	}
	return t, nil
}
