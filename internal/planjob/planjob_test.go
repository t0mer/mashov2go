package planjob

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/crypto"
	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/schedule"
	"github.com/t0mer/mashov2go/internal/store"
)

// fakeSource returns a fixed school day, Friday 2026-11-27 (weekday 6 → Sunday=1 numbering, day=6).
type fakeSource struct {
	lessons  []mashov.Lesson
	holidays []mashov.Holiday
	calls    int
}

func (f *fakeSource) Load(_ context.Context, _ family.Member, day time.Time) (schedule.Input, string, error) {
	f.calls++
	return schedule.Input{
		Date:     day,
		Lessons:  f.lessons,
		Changes:  &mashov.TimetableChanges{},
		Bells:    []mashov.Bell{{LessonNumber: 1, StartTime: "08:00:00", EndTime: "08:45:00"}, {LessonNumber: 2, StartTime: "08:50:00", EndTime: "09:35:00"}},
		Holidays: f.holidays,
	}, "Noa", nil
}

func lesson(n int, subject string) mashov.Lesson {
	return mashov.Lesson{
		TimeTable:    mashov.TimeTableSlot{Day: 6, Lesson: n, GroupID: n * 10, RoomNum: "12"},
		GroupDetails: mashov.GroupDetails{GroupID: n * 10, SubjectName: subject},
	}
}

func setup(t *testing.T, src *fakeSource, childPhone string) (*Service, *store.Store, family.Member) {
	t.Helper()
	ctx := context.Background()
	sealer, err := crypto.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"), sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	child, err := st.CreateMember(ctx, family.Member{Role: family.RoleChild, Name: "Noa", Language: "he",
		Channel: family.ChannelWhatsApp, WhatsAppPhone: childPhone, QuietStart: "21:00", QuietEnd: "07:00",
		PlanEnabled: true, PlanTime: "19:30", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateMember(ctx, family.Member{Role: family.RoleParent, Name: "Tomer", Language: "he",
		Channel: family.ChannelWhatsApp, WhatsAppPhone: "972501111111", QuietStart: "22:30", QuietEnd: "06:30",
		PlanTime: "19:30", Active: true}); err != nil {
		t.Fatal(err)
	}
	return &Service{Store: st, Source: src, Loc: loc}, st, child
}

func at(h, m int) time.Time {
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	return time.Date(2026, 11, 26, h, m, 0, 0, loc)
}

func pendingCount(t *testing.T, st *store.Store) int {
	t.Helper()
	d, err := st.DueDeliveries(context.Background(), at(23, 59).Add(48*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	return len(d)
}

func TestPlanQueuedOnceAndLateChangeUpdates(t *testing.T) {
	src := &fakeSource{lessons: []mashov.Lesson{lesson(1, "Math"), lesson(2, "English")}}
	svc, st, _ := setup(t, src, "972502222222")
	ctx := context.Background()

	svc.Now = func() time.Time { return at(19, 0) }
	if n, err := svc.RunDue(ctx); err != nil || n != 0 || src.calls != 0 {
		t.Fatalf("before plan time: n=%d calls=%d err=%v", n, src.calls, err)
	}

	svc.Now = func() time.Time { return at(19, 30) }
	if n, err := svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("at plan time: n=%d err=%v", n, err)
	}
	// Child only by default for daily_plan; the parent gets nothing.
	if got := pendingCount(t, st); got != 1 {
		t.Fatalf("queued deliveries = %d, want 1 (child only)", got)
	}
	d, _ := st.DueDeliveries(ctx, at(23, 0).Add(24*time.Hour), 10)
	if !strings.Contains(d[0].Body, "Math") || strings.Contains(d[0].Body, "עדכון") {
		t.Fatalf("unexpected body: %q", d[0].Body)
	}

	// Same data again: nothing new.
	svc.Now = func() time.Time { return at(20, 0) }
	if n, _ := svc.RunDue(ctx); n != 0 || pendingCount(t, st) != 1 {
		t.Fatalf("rerun queued again: n=%d", n)
	}

	// A lesson is cancelled-and-added after the plan went out: one update, found
	// once the load gap has passed (the child is not reloaded before then).
	src.lessons = append(src.lessons, lesson(3, "Art"))
	svc.Now = func() time.Time { return at(20, 30) }
	if n, err := svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("late change: n=%d err=%v", n, err)
	}
	if got := pendingCount(t, st); got != 2 {
		t.Fatalf("after update queued = %d, want 2", got)
	}
	d, _ = st.DueDeliveries(ctx, at(23, 0).Add(24*time.Hour), 10)
	if !strings.HasPrefix(d[len(d)-1].Body, "🔔") {
		t.Fatalf("update body missing prefix: %q", d[len(d)-1].Body)
	}
}

func TestHolidayIsSkippedAndNotReloaded(t *testing.T) {
	src := &fakeSource{holidays: []mashov.Holiday{{Name: "Test day", StartDate: "2026-11-27T00:00:00", EndDate: "2026-11-27T00:00:00"}}}
	svc, st, _ := setup(t, src, "972502222222")
	ctx := context.Background()
	svc.Now = func() time.Time { return at(19, 30) }
	if n, err := svc.RunDue(ctx); err != nil || n != 0 {
		t.Fatalf("holiday: n=%d err=%v", n, err)
	}
	if pendingCount(t, st) != 0 {
		t.Fatal("holiday queued a message")
	}
	svc.Now = func() time.Time { return at(20, 0) }
	_, _ = svc.RunDue(ctx)
	if src.calls != 1 {
		t.Fatalf("holiday reloaded Mashov data again: calls=%d", src.calls)
	}
}

func TestChildWithoutPhoneGetsSkippedRow(t *testing.T) {
	src := &fakeSource{lessons: []mashov.Lesson{lesson(1, "Math")}}
	svc, st, _ := setup(t, src, "")
	svc.Now = func() time.Time { return at(19, 30) }
	if _, err := svc.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pendingCount(t, st) != 0 {
		t.Fatal("unreachable child stayed pending")
	}
}

func TestLoadsAreThrottlePerChild(t *testing.T) {
	src := &fakeSource{lessons: []mashov.Lesson{lesson(1, "Math")}}
	svc, _, _ := setup(t, src, "972500000001")
	ctx := context.Background()

	svc.Now = func() time.Time { return at(19, 30) }
	if _, err := svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	// Every tick after plan time until the gap passes must not reach Mashov.
	for _, mins := range []int{45, 55} {
		svc.Now = func() time.Time { return at(19, mins) }
		if _, err := svc.RunDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if src.calls != 1 {
		t.Fatalf("loads within the gap = %d, want 1", src.calls)
	}
}

func TestPrefetchLoadsBeforePlanTimeAndPlanReusesIt(t *testing.T) {
	src := &fakeSource{lessons: []mashov.Lesson{lesson(1, "Math")}}
	svc, st, _ := setup(t, src, "972500000001")
	ctx := context.Background()

	// 19:15 is inside the prefetch window: data is loaded, nothing is queued.
	svc.Now = func() time.Time { return at(19, 15) }
	if n, err := svc.RunDue(ctx); err != nil || n != 0 {
		t.Fatalf("prefetch: n=%d err=%v", n, err)
	}
	if src.calls != 1 || pendingCount(t, st) != 0 {
		t.Fatalf("after prefetch: loads=%d pending=%d", src.calls, pendingCount(t, st))
	}

	// 19:30 plan time: the plan is built from the prefetched data, no second load.
	svc.Now = func() time.Time { return at(19, 30) }
	if n, err := svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("plan: n=%d err=%v", n, err)
	}
	if src.calls != 1 {
		t.Fatalf("plan reloaded Mashov: loads=%d, want 1", src.calls)
	}
	if pendingCount(t, st) != 1 {
		t.Fatalf("pending = %d, want 1", pendingCount(t, st))
	}
}

func TestSendNowQueuesAgainAndLeavesScheduleAlone(t *testing.T) {
	src := &fakeSource{lessons: []mashov.Lesson{lesson(1, "Math")}}
	svc, st, child := setup(t, src, "972500000001")
	ctx := context.Background()

	svc.Now = func() time.Time { return at(19, 30) }
	if _, err := svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	before, _, _ := st.SentPlanFor(ctx, child.ID, "2026-11-27")

	svc.Now = func() time.Time { return at(21, 0) }
	n, err := svc.SendNow(ctx, child, nil)
	if err != nil || n != 1 {
		t.Fatalf("send now: n=%d err=%v", n, err)
	}
	if got := pendingCount(t, st); got != 2 {
		t.Fatalf("pending = %d, want 2 (scheduled plan + manual send)", got)
	}
	after, _, _ := st.SentPlanFor(ctx, child.ID, "2026-11-27")
	if after.Hash != before.Hash || !after.SentAt.Equal(before.SentAt) {
		t.Fatal("manual send changed the scheduled plan record")
	}
}

func TestSendNowRefusesNonSchoolDay(t *testing.T) {
	src := &fakeSource{lessons: []mashov.Lesson{lesson(1, "Math")},
		holidays: []mashov.Holiday{{ID: 1, Name: "Holiday", StartDate: "2026-11-26T00:00:00", EndDate: "2026-11-28T00:00:00"}}}
	svc, st, child := setup(t, src, "972500000001")
	svc.Now = func() time.Time { return at(20, 0) }
	if _, err := svc.SendNow(context.Background(), child, nil); !errors.Is(err, ErrNoSchoolDay) {
		t.Fatalf("err = %v, want ErrNoSchoolDay", err)
	}
	if pendingCount(t, st) != 0 {
		t.Fatal("a refused send queued something")
	}
}
