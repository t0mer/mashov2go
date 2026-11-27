package gradesjob

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
	"github.com/t0mer/mashov2go/internal/store"
	"github.com/t0mer/mashov2go/internal/syncer"
)

// fakeSource returns the grades set for each child, keyed by child member id.
type fakeSource struct {
	grades map[int64][]mashov.Grade
	fail   map[int64]bool
}

func (f *fakeSource) Grades(_ context.Context, child family.Member, _, _ time.Time) ([]mashov.Grade, error) {
	if f.fail[child.ID] {
		return nil, errors.New("mashov unavailable")
	}
	return f.grades[child.ID], nil
}

type env struct {
	svc    *Service
	st     *store.Store
	src    *fakeSource
	kid    family.Member
	parent family.Member
	now    time.Time
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	sealer, _ := crypto.NewSealer(make([]byte, 32))
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "g.db"), sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	acc, _ := st.CreateAccount(ctx, store.Account{Name: "home", Semel: 1, Year: 2027, Username: "u", DeviceUUID: "d"})
	kid, _ := st.CreateMember(ctx, family.Member{Role: family.RoleChild, Name: "Demo Child", Language: "he",
		Channel: family.ChannelWhatsApp, WhatsAppPhone: "972500000002", QuietStart: "21:00", QuietEnd: "07:00",
		PlanTime: "19:30", Active: true})
	_ = st.LinkChild(ctx, kid.ID, acc.ID, "guid-1")
	par, _ := st.CreateMember(ctx, family.Member{Role: family.RoleParent, Name: "Parent", Language: "he",
		Channel: family.ChannelWhatsApp, WhatsAppPhone: "972500000001", QuietStart: "22:30", QuietEnd: "06:30",
		PlanTime: "19:30", Active: true})
	src := &fakeSource{grades: map[int64][]mashov.Grade{}, fail: map[int64]bool{}}
	e := &env{st: st, src: src, kid: kid, parent: par, now: time.Date(2026, 11, 27, 10, 0, 0, 0, loc)}
	e.svc = &Service{
		Store: st, Source: src, Loc: loc,
		Syncer: &syncer.Syncer{Store: st, Now: func() time.Time { return e.now }},
		Now:    func() time.Time { return e.now },
		Gap:    func() time.Duration { return 0 },
	}
	return e
}

func (e *env) pending(t *testing.T) []store.Delivery {
	t.Helper()
	d, err := e.st.DueDeliveries(context.Background(), e.now.Add(48*time.Hour), 50)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFirstSyncIsSilentThenNewGradeReachesParents(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.src.grades[e.kid.ID] = []mashov.Grade{{ID: 1, GradingEventID: 9, GradingEvent: "Quiz", SubjectName: "Math", Rate: 80}}

	if n, err := e.svc.RunDue(ctx); err != nil || n != 0 {
		t.Fatalf("baseline: n=%d err=%v", n, err)
	}
	if len(e.pending(t)) != 0 {
		t.Fatal("existing grades were announced on the first sync")
	}

	e.now = e.now.Add(time.Hour)
	e.src.grades[e.kid.ID] = append(e.src.grades[e.kid.ID], mashov.Grade{ID: 2, GradingEventID: 9, GradingEvent: "Quiz", SubjectName: "English", RangeGrade: "מצוין"})
	if n, err := e.svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("new grade: n=%d err=%v", n, err)
	}
	d := e.pending(t)
	if len(d) != 1 || d[0].MemberID != e.parent.ID {
		t.Fatalf("deliveries = %+v (want one, to the parent)", d)
	}
	if !strings.Contains(d[0].Body, "ציון חדש") || !strings.Contains(d[0].Body, "English") || !strings.Contains(d[0].Body, "מצוין") {
		t.Fatalf("body = %q", d[0].Body)
	}
}

func TestChangedGradeIsAnUpdate(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.src.grades[e.kid.ID] = []mashov.Grade{{ID: 1, GradingEventID: 9, SubjectName: "Math", Rate: 80}}
	_, _ = e.svc.RunDue(ctx)

	e.now = e.now.Add(time.Hour)
	e.src.grades[e.kid.ID] = []mashov.Grade{{ID: 1, GradingEventID: 9, SubjectName: "Math", Rate: 85}}
	if n, err := e.svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("update: n=%d err=%v", n, err)
	}
	d := e.pending(t)
	if len(d) != 1 || !strings.Contains(d[0].Body, "ציון עודכן") || !strings.Contains(d[0].Body, "85") {
		t.Fatalf("update delivery = %+v", d)
	}
}

func TestMutedParentGetsNothingAndFailingChildDoesNotStopOthers(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	other, _ := e.st.CreateMember(ctx, family.Member{Role: family.RoleChild, Name: "Other", Language: "he",
		Channel: family.ChannelWhatsApp, WhatsAppPhone: "972500000003", QuietStart: "21:00", QuietEnd: "07:00",
		PlanTime: "19:30", Active: true})
	acc, _ := e.st.CreateAccount(ctx, store.Account{Name: "b", Semel: 2, Year: 2027, Username: "v", DeviceUUID: "e"})
	_ = e.st.LinkChild(ctx, other.ID, acc.ID, "guid-2")
	if err := e.st.SetMute(ctx, e.parent.ID, e.kid.ID, family.CatGrades, true); err != nil {
		t.Fatal(err)
	}
	e.src.fail[e.kid.ID] = true
	e.src.grades[other.ID] = []mashov.Grade{{ID: 5, GradingEventID: 1, SubjectName: "Art", Rate: 90}}
	_, err := e.svc.RunDue(ctx)
	if err == nil {
		t.Fatal("the failing child's error was not reported")
	}
	// Other child: baseline on this run, so nothing queued; the failure is isolated.
	e.now = e.now.Add(time.Hour)
	e.src.grades[other.ID] = append(e.src.grades[other.ID], mashov.Grade{ID: 6, GradingEventID: 1, SubjectName: "Art", Rate: 91})
	// The failing child still errors, and the other child's grade is still queued.
	if n, err := e.svc.RunDue(ctx); n != 1 || err == nil {
		t.Fatalf("other child: n=%d err=%v (want 1 and the failing child's error)", n, err)
	}
	for _, d := range e.pending(t) {
		if d.MemberID == e.parent.ID {
			t.Fatal("muted parent received a grade")
		}
	}
}

func TestRespectsGap(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Gap = func() time.Duration { return time.Hour }
	e.src.grades[e.kid.ID] = []mashov.Grade{{ID: 1, GradingEventID: 9, SubjectName: "Math", Rate: 80}}
	_, _ = e.svc.RunDue(ctx)
	e.src.grades[e.kid.ID] = append(e.src.grades[e.kid.ID], mashov.Grade{ID: 2, GradingEventID: 9, SubjectName: "x", Rate: 1})
	e.now = e.now.Add(30 * time.Minute)
	if n, _ := e.svc.RunDue(ctx); n != 0 {
		t.Fatal("fetched again inside the gap")
	}
	e.now = e.now.Add(31 * time.Minute)
	if n, _ := e.svc.RunDue(ctx); n != 1 {
		t.Fatalf("not fetched after the gap: n=%d", n)
	}
}
