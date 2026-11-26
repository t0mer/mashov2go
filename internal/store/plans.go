package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SentPlan is the record of a daily plan sent to one member for one date.
type SentPlan struct {
	MemberID int64
	Date     string // YYYY-MM-DD, the school day the plan is for
	Hash     string
	SentAt   time.Time
}

// SentPlanFor returns the plan recorded for member and date, if any.
func (s *Store) SentPlanFor(ctx context.Context, memberID int64, date string) (SentPlan, bool, error) {
	var p SentPlan
	var sent string
	err := s.db.QueryRowContext(ctx,
		`SELECT member_id, plan_date, content_hash, sent_at FROM plans_sent WHERE member_id = ? AND plan_date = ?`,
		memberID, date).Scan(&p.MemberID, &p.Date, &p.Hash, &sent)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	if err != nil {
		return p, false, err
	}
	p.SentAt, _ = time.Parse(ts, sent)
	return p, true, nil
}

// SavePlanSent records (or replaces) the plan that was sent for member and date.
func (s *Store) SavePlanSent(ctx context.Context, p SentPlan) error {
	if p.SentAt.IsZero() {
		p.SentAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO plans_sent(member_id, plan_date, content_hash, sent_at) VALUES(?,?,?,?)
		 ON CONFLICT(member_id, plan_date) DO UPDATE SET content_hash = excluded.content_hash, sent_at = excluded.sent_at`,
		p.MemberID, p.Date, p.Hash, p.SentAt.UTC().Format(ts))
	return err
}
