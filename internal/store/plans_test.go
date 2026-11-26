package store

import (
	"context"
	"testing"
)

func TestSavePlanSentUpsertsPerMemberAndDate(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	m, err := s.CreateMember(ctx, child("Noa"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.SentPlanFor(ctx, m.ID, "2026-11-27"); ok {
		t.Fatal("plan reported before any was saved")
	}
	if err := s.SavePlanSent(ctx, SentPlan{MemberID: m.ID, Date: "2026-11-27", Hash: "h1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePlanSent(ctx, SentPlan{MemberID: m.ID, Date: "2026-11-27", Hash: "h2"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.SentPlanFor(ctx, m.ID, "2026-11-27")
	if err != nil || !ok || got.Hash != "h2" {
		t.Fatalf("after update: %+v ok=%v err=%v", got, ok, err)
	}
}
