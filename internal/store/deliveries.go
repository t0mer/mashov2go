package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Delivery is one event to one member over one channel (CLAUDE.md §8.5).
type Delivery struct {
	ID        int64
	EventID   int64
	MemberID  int64
	Channel   string // "whatsapp" | "telegram"
	Status    string // pending | sent | failed | skipped_no_channel
	Attempts  int
	NextAt    time.Time
	LastError string
	Body      string
}

const ts = time.RFC3339Nano

// EnqueueDelivery inserts a pending delivery. It returns false when the same
// (event, member) pair already has one, so an event is never sent twice to a person.
func (s *Store) EnqueueDelivery(ctx context.Context, d Delivery) (bool, error) {
	if d.NextAt.IsZero() {
		d.NextAt = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO deliveries(event_id, member_id, channel, status, attempts, next_at, body)
		 VALUES(?,?,?,?,?,?,?)`,
		d.EventID, d.MemberID, d.Channel, "pending", 0, d.NextAt.UTC().Format(ts), d.Body)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// DueDeliveries returns pending deliveries whose next attempt time has passed, oldest first.
func (s *Store) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_id, member_id, channel, status, attempts, next_at, last_error, body
		   FROM deliveries WHERE status = 'pending' AND next_at <= ?
		  ORDER BY next_at, id LIMIT ?`, now.UTC().Format(ts), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		var next string
		if err := rows.Scan(&d.ID, &d.EventID, &d.MemberID, &d.Channel, &d.Status, &d.Attempts, &next, &d.LastError, &d.Body); err != nil {
			return nil, err
		}
		if d.NextAt, err = time.Parse(ts, next); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkDeliverySent records a successful send.
func (s *Store) MarkDeliverySent(ctx context.Context, id int64, attempts int) error {
	return s.setDelivery(ctx, id, "sent", attempts, time.Time{}, "")
}

// MarkDeliveryRetry keeps the delivery pending for another attempt at next.
func (s *Store) MarkDeliveryRetry(ctx context.Context, id int64, attempts int, next time.Time, lastErr string) error {
	return s.setDelivery(ctx, id, "pending", attempts, next, lastErr)
}

// MarkDeliveryFailed ends the delivery; the UI shows it with a Retry action.
func (s *Store) MarkDeliveryFailed(ctx context.Context, id int64, attempts int, lastErr string) error {
	return s.setDelivery(ctx, id, "failed", attempts, time.Time{}, lastErr)
}

// MarkDeliverySkipped records that the member had no usable channel.
func (s *Store) MarkDeliverySkipped(ctx context.Context, id int64, reason string) error {
	return s.setDelivery(ctx, id, "skipped_no_channel", 0, time.Time{}, reason)
}

// SwitchDeliveryChannel moves a failed delivery to the fallback channel and makes
// it due now. It only applies when the delivery is still on fromChannel, so a
// fallback is used at most once.
func (s *Store) SwitchDeliveryChannel(ctx context.Context, id int64, fromChannel, toChannel string, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE deliveries SET channel = ?, status = 'pending', attempts = 0, next_at = ?, last_error = ''
		  WHERE id = ? AND channel = ?`,
		toChannel, now.UTC().Format(ts), id, fromChannel)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) setDelivery(ctx context.Context, id int64, status string, attempts int, next time.Time, lastErr string) error {
	// next_at is NOT NULL. Terminal states store the time of the change, which
	// is never read again because only pending rows are picked up.
	if next.IsZero() {
		next = time.Now()
	}
	nextStr := next.UTC().Format(ts)
	res, err := s.db.ExecContext(ctx,
		`UPDATE deliveries SET status = ?, attempts = ?, next_at = ?, last_error = ? WHERE id = ?`,
		status, attempts, nextStr, truncate(lastErr, 500), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetDelivery returns one delivery.
func (s *Store) GetDelivery(ctx context.Context, id int64) (Delivery, error) {
	var d Delivery
	var next string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, event_id, member_id, channel, status, attempts, next_at, last_error, body FROM deliveries WHERE id = ?`, id).
		Scan(&d.ID, &d.EventID, &d.MemberID, &d.Channel, &d.Status, &d.Attempts, &next, &d.LastError, &d.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.NextAt, _ = time.Parse(ts, next)
	return d, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
