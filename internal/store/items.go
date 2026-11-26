package store

import (
	"context"
	"time"
)

// Item is one synced Mashov record of a (child, domain).
type Item struct {
	AccountID   int64
	ChildGUID   string
	Domain      string
	Key         string
	ContentHash string
	Payload     string // JSON
	FirstSeen   time.Time
	LastSeen    time.Time
}

// ListItems returns the live (non-deleted) items of a child's domain.
func (s *Store) ListItems(ctx context.Context, accountID int64, childGUID, domain string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_key, content_hash, payload, first_seen, last_seen FROM items
		 WHERE account_id = ? AND child_guid = ? AND domain = ? AND deleted_at IS NULL`,
		accountID, childGUID, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it := Item{AccountID: accountID, ChildGUID: childGUID, Domain: domain}
		var first, last string
		if err := rows.Scan(&it.Key, &it.ContentHash, &it.Payload, &first, &last); err != nil {
			return nil, err
		}
		it.FirstSeen, _ = time.Parse(time.RFC3339, first)
		it.LastSeen, _ = time.Parse(time.RFC3339, last)
		out = append(out, it)
	}
	return out, rows.Err()
}

// PutItem inserts or updates an item, keeping first_seen and clearing deleted_at.
func (s *Store) PutItem(ctx context.Context, it Item, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO items(account_id, child_guid, domain, item_key, content_hash, payload, first_seen, last_seen, deleted_at)
		VALUES(?,?,?,?,?,?,?,?,NULL)
		ON CONFLICT(account_id, child_guid, domain, item_key) DO UPDATE SET
		  content_hash = excluded.content_hash,
		  payload      = excluded.payload,
		  last_seen    = excluded.last_seen,
		  deleted_at   = NULL`,
		it.AccountID, it.ChildGUID, it.Domain, it.Key, it.ContentHash, it.Payload, ts, ts)
	return err
}

// SoftDeleteItem marks an item gone. No event is produced for removals (CLAUDE.md §5.3).
func (s *Store) SoftDeleteItem(ctx context.Context, accountID int64, childGUID, domain, key string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE items SET deleted_at = ? WHERE account_id = ? AND child_guid = ? AND domain = ? AND item_key = ?`,
		now.UTC().Format(time.RFC3339), accountID, childGUID, domain, key)
	return err
}

// IsBaselined reports whether a (child, domain) has completed its first silent sync.
func (s *Store) IsBaselined(ctx context.Context, accountID int64, childGUID, domain string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM domain_state WHERE account_id = ? AND child_guid = ? AND domain = ?`,
		accountID, childGUID, domain).Scan(&n)
	return n > 0, err
}

// MarkSynced records a successful sync of a (child, domain) and marks it baselined.
func (s *Store) MarkSynced(ctx context.Context, accountID int64, childGUID, domain string, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO domain_state(account_id, child_guid, domain, baselined_at, last_success_at) VALUES(?,?,?,?,?)
		ON CONFLICT(account_id, child_guid, domain) DO UPDATE SET last_success_at = excluded.last_success_at`,
		accountID, childGUID, domain, ts, ts)
	return err
}
