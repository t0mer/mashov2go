package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDeliveryListAndRetry(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	kid, _ := s.CreateMember(ctx, child("Noa"))
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	id1 := mustEnqueue(t, s, kid.ID, 1)
	id2 := mustEnqueue(t, s, kid.ID, 2)
	if err := s.MarkDeliveryFailed(ctx, id2, 6, "boom"); err != nil {
		t.Fatal(err)
	}

	failed, _ := s.ListDeliveries(ctx, "failed", 10)
	if len(failed) != 1 || failed[0].ID != id2 {
		t.Fatalf("failed list = %+v", failed)
	}
	all, _ := s.ListDeliveries(ctx, "", 10)
	if len(all) != 2 || all[0].ID != id2 {
		t.Fatalf("all list (newest first) = %+v", all)
	}

	if err := s.RetryDelivery(ctx, id2, now); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	d, _ := s.GetDelivery(ctx, id2)
	if d.Status != "pending" || d.Attempts != 0 || d.LastError != "" {
		t.Fatalf("after retry: %+v", d)
	}
	if err := s.RetryDelivery(ctx, id1, now); !errors.Is(err, ErrNotRetriable) {
		t.Fatalf("pending retry err = %v", err)
	}
	if err := s.RetryDelivery(ctx, 999, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing retry err = %v", err)
	}
}
func mustEnqueue(t *testing.T, s *Store, memberID int64, n int) int64 {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	key := "event-" + string(rune('a'+n))
	if _, err := s.InsertEvent(ctx, Event{Category: "homework", Kind: "new", OccurredAt: now, DetectedAt: now, Source: "diff", DedupeKey: key}); err != nil {
		t.Fatal(err)
	}
	eventID, ok, err := s.EventIDByDedupe(ctx, key)
	if err != nil || !ok {
		t.Fatalf("event for %s: %v %v", key, ok, err)
	}
	if _, err := s.EnqueueDelivery(ctx, Delivery{EventID: eventID, MemberID: memberID, Channel: "whatsapp", NextAt: now, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM deliveries WHERE event_id = ? AND member_id = ?`, eventID, memberID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
