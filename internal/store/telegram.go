package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrBindInvalid is returned when a bind token is unknown, used, expired, or its
// member no longer exists. The reason is deliberately not distinguished.
var ErrBindInvalid = errors.New("telegram bind token is invalid")

// HashBindToken is the value stored for a bind token.
func HashBindToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// CreateBindToken stores a new bind token for a member. Earlier unused tokens of
// the same member are removed, so only the latest deep link works.
func (s *Store) CreateBindToken(ctx context.Context, memberID int64, value string, expiresAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM tg_link_tokens WHERE member_id = ? AND used_at IS NULL`, memberID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tg_link_tokens(token_hash, member_id, expires_at) VALUES(?,?,?)`,
		HashBindToken(value), memberID, expiresAt.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("storing bind token: %w", err)
	}
	return tx.Commit()
}

// ConsumeBindToken spends a bind token and binds chatID to its member. The token
// works once: the update that marks it used and the member update share one
// transaction, so a repeated /start cannot bind again.
func (s *Store) ConsumeBindToken(ctx context.Context, value string, chatID int64, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var memberID int64
	var expires string
	err = tx.QueryRowContext(ctx,
		`SELECT member_id, expires_at FROM tg_link_tokens WHERE token_hash = ? AND used_at IS NULL`,
		HashBindToken(value)).Scan(&memberID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrBindInvalid
	}
	if err != nil {
		return 0, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || !now.Before(exp) {
		return 0, ErrBindInvalid
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE members SET telegram_chat_id = ? WHERE id = ? AND deleted_at IS NULL`, chatID, memberID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, ErrBindInvalid
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tg_link_tokens SET used_at = ? WHERE token_hash = ?`,
		now.UTC().Format(time.RFC3339), HashBindToken(value)); err != nil {
		return 0, err
	}
	return memberID, tx.Commit()
}
