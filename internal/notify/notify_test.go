package notify

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/crypto"
	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/routing"
	"github.com/t0mer/mashov2go/internal/store"
)

func TestEnqueueHoldsQuietHoursAndSkipsNoChannel(t *testing.T) {
	ctx := context.Background()
	sealer, _ := crypto.NewSealer(make([]byte, 32))
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "n.db"), sealer)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	m, err := st.CreateMember(ctx, family.Member{Role: family.RoleParent, Name: "P", Language: "he",
		Channel: family.ChannelWhatsApp, WhatsAppPhone: "972500000009", QuietStart: "22:30", QuietEnd: "06:30",
		PlanTime: "19:30", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 27, 23, 0, 0, 0, loc)
	if _, err := st.InsertEvent(ctx, store.Event{Category: "grades", Kind: "new", OccurredAt: now, DetectedAt: now, Source: "diff", DedupeKey: "e1"}); err != nil {
		t.Fatal(err)
	}
	eid, _, _ := st.EventIDByDedupe(ctx, "e1")

	if err := Enqueue(ctx, st, loc, eid, routing.Target{Member: m, Channel: family.ChannelWhatsApp}, "held", now); err != nil {
		t.Fatal(err)
	}
	due, _ := st.DueDeliveries(ctx, now, 10)
	if len(due) != 0 {
		t.Fatal("message in quiet hours was not held")
	}
	later := time.Date(2026, 11, 28, 6, 30, 0, 0, loc)
	due, _ = st.DueDeliveries(ctx, later, 10)
	if len(due) != 1 {
		t.Fatalf("held message not due at quiet end: %d", len(due))
	}

	// A second event: one delivery per event and member.
	if _, err := st.InsertEvent(ctx, store.Event{Category: "grades", Kind: "new", OccurredAt: now, DetectedAt: now, Source: "diff", DedupeKey: "e2"}); err != nil {
		t.Fatal(err)
	}
	eid2, _, _ := st.EventIDByDedupe(ctx, "e2")
	if err := Enqueue(ctx, st, loc, eid2, routing.Target{Member: m, Channel: "", SkipReason: "no_channel"}, "x", now); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListDeliveries(ctx, "skipped_no_channel", 10)
	if len(list) != 1 || list[0].LastError != "no_channel" {
		t.Fatalf("skipped = %+v", list)
	}
}
