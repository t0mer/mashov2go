package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Event is the envelope stored for every change Holocron reports (CLAUDE.md §5.4).
type Event struct {
	ID         int64
	Category   string
	Kind       string
	ChildGUID  string
	OccurredAt time.Time
	DetectedAt time.Time
	Data       string // JSON
	Source     string // notification | diff | schedule
	DedupeKey  string
}

// InsertEvent stores an event once. It reports whether a new row was written;
// a repeated dedupe key is a no-op so the same change never notifies twice.
func (s *Store) InsertEvent(ctx context.Context, e Event) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO events(category, kind, child_guid, occurred_at, detected_at, data, source, dedupe_key)
		VALUES(?,?,?,?,?,?,?,?)`,
		e.Category, e.Kind, e.ChildGUID,
		e.OccurredAt.UTC().Format(time.RFC3339), e.DetectedAt.UTC().Format(time.RFC3339),
		e.Data, e.Source, e.DedupeKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ListEvents returns events detected at or after since, newest first.
func (s *Store) ListEvents(ctx context.Context, since time.Time, limit int) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, category, kind, child_guid, occurred_at, detected_at, data, source, dedupe_key
		FROM events WHERE detected_at >= ? ORDER BY id DESC LIMIT ?`,
		since.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var occ, det string
		if err := rows.Scan(&e.ID, &e.Category, &e.Kind, &e.ChildGUID, &occ, &det, &e.Data, &e.Source, &e.DedupeKey); err != nil {
			return nil, err
		}
		e.OccurredAt, _ = time.Parse(time.RFC3339, occ)
		e.DetectedAt, _ = time.Parse(time.RFC3339, det)
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventIDByDedupe returns the id of the event stored under key, if any.
func (s *Store) EventIDByDedupe(ctx context.Context, key string) (int64, bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM events WHERE dedupe_key = ?`, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}
