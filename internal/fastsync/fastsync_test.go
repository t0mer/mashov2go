package fastsync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/crypto"
	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/store"
)

type fakeSource struct{ items []mashov.Notification }

func (f *fakeSource) Notifications(context.Context, int64) ([]mashov.Notification, error) {
	return f.items, nil
}

type invalidation struct {
	member int64
	domain string
}

type fakeChild struct{ calls []invalidation }

func (f *fakeChild) Invalidate(member int64, domain string) {
	f.calls = append(f.calls, invalidation{member, domain})
}

func note(id, typ int, user string) mashov.Notification {
	return mashov.Notification{ID: mashov.Number(id), EventType: mashov.Number(typ), EventUserID: user}
}

type env struct {
	svc   *Service
	src   *fakeSource
	child *fakeChild
	kid   family.Member
	now   time.Time
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	sealer, _ := crypto.NewSealer(make([]byte, 32))
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "f.db"), sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	acc, _ := st.CreateAccount(ctx, store.Account{Name: "home", Semel: 1, Year: 2027, Username: "u", DeviceUUID: "d"})
	kid, _ := st.CreateMember(ctx, family.Member{Role: family.RoleChild, Name: "Demo Child", Language: "he",
		Channel: family.ChannelWhatsApp, WhatsAppPhone: "972500000002", QuietStart: "21:00", QuietEnd: "07:00",
		PlanTime: "19:30", Active: true})
	_ = st.LinkChild(ctx, kid.ID, acc.ID, "sg-1")
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	e := &env{src: &fakeSource{}, child: &fakeChild{}, kid: kid, now: time.Date(2026, 11, 27, 10, 0, 0, 0, loc)}
	e.svc = &Service{Store: st, Source: e.src, Child: e.child, Loc: loc,
		Now:      func() time.Time { return e.now },
		Interval: func() time.Duration { return 0 }}
	return e
}

func TestFirstPollIsBaselineThenNotificationsInvalidate(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.src.items = []mashov.Notification{note(10, 111, "sg-1"), note(11, 101, "sg-1")}
	if n, err := e.svc.RunDue(ctx); err != nil || n != 0 || len(e.child.calls) != 0 {
		t.Fatalf("baseline: n=%d err=%v calls=%v", n, err, e.child.calls)
	}

	e.now = e.now.Add(5 * time.Minute)
	e.src.items = []mashov.Notification{note(10, 111, "sg-1"), note(11, 101, "sg-1"), note(12, 111, "sg-1"), note(13, 102, "sg-1")}
	// 10 and 11 were recorded by the baseline. 12 is a grade and 13 is homework; both map to a domain.
	n, err := e.svc.RunDue(ctx)
	if err != nil || n != 2 {
		t.Fatalf("new items: n=%d err=%v", n, err)
	}
	want := []invalidation{{e.kid.ID, "grades"}, {e.kid.ID, "homework"}}
	if len(e.child.calls) != 2 || e.child.calls[0] != want[0] || e.child.calls[1] != want[1] {
		t.Fatalf("calls = %v, want grades then homework", e.child.calls)
	}
	// Already seen ids are not processed again.
	e.now = e.now.Add(5 * time.Minute)
	if n, _ := e.svc.RunDue(ctx); n != 0 {
		t.Fatalf("seen notifications processed again: n=%d", n)
	}
}

func TestOtherStudentsAreIgnored(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.src.items = []mashov.Notification{note(1, 111, "not-a-linked-student")}
	_, _ = e.svc.RunDue(ctx)
	e.now = e.now.Add(5 * time.Minute)
	e.src.items = append(e.src.items, note(2, 111, "not-a-linked-student"))
	if n, err := e.svc.RunDue(ctx); err != nil || n != 0 {
		t.Fatalf("unlinked student: n=%d err=%v", n, err)
	}
}

func TestOnlyInsideActiveHours(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.src.items = []mashov.Notification{note(1, 111, "sg-1")}
	_, _ = e.svc.RunDue(ctx)
	e.now = time.Date(2026, 11, 27, 23, 30, 0, 0, e.svc.Loc)
	e.src.items = append(e.src.items, note(2, 111, "sg-1"))
	if n, _ := e.svc.RunDue(ctx); n != 0 {
		t.Fatalf("polled outside active hours: n=%d", n)
	}
	e.now = time.Date(2026, 11, 28, 7, 0, 0, 0, e.svc.Loc)
	if n, err := e.svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("inside active hours: n=%d err=%v", n, err)
	}
}

func TestIntervalBetweenPolls(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Interval = func() time.Duration { return 5 * time.Minute }
	e.src.items = []mashov.Notification{note(1, 111, "sg-1")}
	_, _ = e.svc.RunDue(ctx)
	e.src.items = append(e.src.items, note(2, 111, "sg-1"))
	e.now = e.now.Add(2 * time.Minute)
	if n, _ := e.svc.RunDue(ctx); n != 0 {
		t.Fatal("polled again inside the interval")
	}
	e.now = e.now.Add(4 * time.Minute)
	if n, err := e.svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("after the interval: n=%d err=%v", n, err)
	}
}
