// Package notify queues one routed message for one recipient: it holds the send
// until the recipient's quiet hours end and records targets without a usable
// channel as skipped (CLAUDE.md §6.3, §6.4). Plans and grades both use it.
package notify

import (
	"context"
	"time"

	"github.com/t0mer/mashov2go/internal/routing"
	"github.com/t0mer/mashov2go/internal/store"
)

// Enqueue queues one delivery of eventID to target t with body text. A duplicate
// for the same event and member is a no-op.
func Enqueue(ctx context.Context, st *store.Store, loc *time.Location, eventID int64, t routing.Target, body string, now time.Time) error {
	when := now
	if t.Member.QuietStart != "" && t.Member.QuietEnd != "" {
		until, held, err := routing.HoldUntil(now, t.Member.QuietStart, t.Member.QuietEnd, loc)
		if err != nil {
			return err
		}
		if held {
			when = until
		}
	}
	d := store.Delivery{EventID: eventID, MemberID: t.Member.ID, Channel: t.Channel, Body: body, NextAt: when}
	if t.SkipReason != "" {
		d.Status, d.LastError = "skipped_no_channel", t.SkipReason
	}
	_, err := st.EnqueueDelivery(ctx, d)
	return err
}
