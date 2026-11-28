package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotRetriable is returned when a delivery is not in a state that can be retried.
var ErrNotRetriable = errors.New("delivery is not failed or skipped")

// ListDeliveries returns deliveries, newest first. An empty status lists all.
func (s *Store) ListDeliveries(ctx context.Context, status string, limit int) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_id, member_id, channel, status, attempts, next_at, last_error, body
		   FROM deliveries WHERE (? = '' OR status = ?) ORDER BY id DESC LIMIT ?`,
		status, status, limit)
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
		d.NextAt, _ = time.Parse(ts, next)
		out = append(out, d)
	}
	return out, rows.Err()
}

// RetryDelivery puts a failed or skipped delivery back in the queue with its
// attempts reset. ErrNotFound when it does not exist; ErrNotRetriable when it
// is pending or already sent.
func (s *Store) RetryDelivery(ctx context.Context, id int64, now time.Time) error {
	d, err := s.GetDelivery(ctx, id)
	if err != nil {
		return err
	}
	if d.Status != "failed" && d.Status != "skipped_no_channel" {
		return ErrNotRetriable
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE deliveries SET status = 'pending', attempts = 0, next_at = ?, last_error = '' WHERE id = ?`,
		now.UTC().Format(ts), id)
	return err
}
