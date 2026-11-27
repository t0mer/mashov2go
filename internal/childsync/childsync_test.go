package childsync

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
	"github.com/t0mer/mashov2go/internal/templates"
)

// fakeSource returns the records set for each child, keyed by child member id.
type fakeSource struct {
	grades   map[int64][]mashov.Grade
	behave   map[int64][]mashov.Behave
	homework map[int64][]mashov.HomeworkItem
	fail     map[int64]bool
}

func (f *fakeSource) Grades(_ context.Context, child family.Member, _, _ time.Time) ([]mashov.Grade, error) {
	if f.fail[child.ID] {
		return nil, errors.New("mashov unavailable")
	}
	return f.grades[child.ID], nil
}

func (f *fakeSource) Homework(_ context.Context, child family.Member) ([]mashov.HomeworkItem, error) {
	if f.fail[child.ID] {
		return nil, errors.New("mashov unavailable")
	}
	return f.homework[child.ID], nil
}

func (f *fakeSource) Behave(_ context.Context, child family.Member, _, _ time.Time) ([]mashov.Behave, error) {
	if f.fail[child.ID] {
		return nil, errors.New("mashov unavailable")
	}
	return f.behave[child.ID], nil
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
	src := &fakeSource{grades: map[int64][]mashov.Grade{}, behave: map[int64][]mashov.Behave{}, homework: map[int64][]mashov.HomeworkItem{}, fail: map[int64]bool{}}
	e := &env{st: st, src: src, kid: kid, parent: par, now: time.Date(2026, 11, 27, 10, 0, 0, 0, loc)}
	e.svc = &Service{
		Store: st, Loc: loc, Domains: []Domain{GradesDomain(src), BehaveDomain(src), HomeworkDomain(src)},
		Templates: templates.Renderer{Store: st},
		Syncer:    &syncer.Syncer{Store: st, Now: func() time.Time { return e.now }},
		Now:       func() time.Time { return e.now },
		Gap:       func() time.Duration { return 0 },
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
	// The mute is for the first child only: the parent gets the other child's grade
	// and nothing for the muted child.
	var toParent []store.Delivery
	for _, d := range e.pending(t) {
		if d.MemberID == e.parent.ID {
			toParent = append(toParent, d)
		}
	}
	if len(toParent) != 1 || !strings.Contains(toParent[0].Body, "Art") {
		t.Fatalf("parent deliveries = %+v (want only the other child's Art grade)", toParent)
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

func TestNewBehaveRecordUsesSchoolWordingAndMuteIsPerCategory(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.src.behave[e.kid.ID] = []mashov.Behave{}
	_, _ = e.svc.RunDue(ctx) // baseline

	e.now = e.now.Add(time.Hour)
	e.src.behave[e.kid.ID] = []mashov.Behave{{AchvaName: "איחור", LessonID: 7, EventCode: 21, Subject: "Math",
		Lesson: 3, LessonDate: "2026-11-02T00:00:00", Justification: "bus"}}
	if n, err := e.svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("behave: n=%d err=%v", n, err)
	}
	d := e.pending(t)
	if len(d) != 1 || d[0].MemberID != e.parent.ID {
		t.Fatalf("deliveries = %+v", d)
	}
	for _, want := range []string{"איחור", "Math", "שיעור 3", "02/11", "bus"} {
		if !strings.Contains(d[0].Body, want) {
			t.Fatalf("body missing %q: %q", want, d[0].Body)
		}
	}

	// Muting behaviour silences behaviour only: a new grade still reaches the parent.
	if err := e.st.SetMute(ctx, e.parent.ID, e.kid.ID, family.CatBehave, true); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Hour)
	e.src.behave[e.kid.ID] = append(e.src.behave[e.kid.ID], mashov.Behave{AchvaName: "הפרעה", LessonID: 8, EventCode: 30, Subject: "Art", Lesson: 1, LessonDate: "2026-11-03T00:00:00"})
	e.src.grades[e.kid.ID] = []mashov.Grade{{ID: 1, GradingEventID: 9, SubjectName: "Math", Rate: 80}}
	if _, err := e.svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	for _, x := range e.pending(t) {
		if strings.Contains(x.Body, "הפרעה") {
			t.Fatal("muted behaviour category reached the parent")
		}
	}
}

func TestInvalidateFetchesInsideTheGap(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Gap = func() time.Duration { return 24 * time.Hour }
	e.src.grades[e.kid.ID] = []mashov.Grade{{ID: 1, GradingEventID: 9, SubjectName: "Math", Rate: 80}}
	_, _ = e.svc.RunDue(ctx)

	e.now = e.now.Add(time.Minute)
	e.src.grades[e.kid.ID] = append(e.src.grades[e.kid.ID], mashov.Grade{ID: 2, GradingEventID: 9, SubjectName: "Art", Rate: 70})
	if n, _ := e.svc.RunDue(ctx); n != 0 {
		t.Fatal("fetched again inside the gap without an invalidation")
	}
	e.svc.Invalidate(e.kid.ID, "grades")
	if n, err := e.svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("after invalidate: n=%d err=%v", n, err)
	}
}

func TestNotificationTypesMapToDomains(t *testing.T) {
	if got := NotificationDomains(111); len(got) != 1 || got[0] != "grades" {
		t.Fatalf("111 -> %v", got)
	}
	if got := NotificationDomains(101); len(got) != 1 || got[0] != "behave" {
		t.Fatalf("101 -> %v", got)
	}
	if got := NotificationDomains(102); len(got) != 1 || got[0] != "homework" {
		t.Fatalf("102 -> %v", got)
	}
}

func TestNewHomeworkGoesToTheChildNotTheParent(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, _ = e.svc.RunDue(ctx) // baseline: the existing homework is not announced
	e.now = e.now.Add(time.Hour)
	e.src.homework[e.kid.ID] = []mashov.HomeworkItem{{LessonID: 5, SubjectName: "Math", Homework: "pages 40-42", LessonDate: "2026-11-27T00:00:00"}}
	if n, err := e.svc.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("homework: n=%d err=%v", n, err)
	}
	d := e.pending(t)
	if len(d) != 1 || d[0].MemberID != e.kid.ID {
		t.Fatalf("deliveries = %+v (want one, to the child)", d)
	}
	if !strings.Contains(d[0].Body, "pages 40-42") || !strings.Contains(d[0].Body, "27/11") {
		t.Fatalf("body = %q", d[0].Body)
	}
}

func TestStoredTemplateOverrideChangesTheMessage(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if err := e.st.PutTemplate(ctx, store.Template{Category: "grades", Language: "he", Audience: "parent", Body: "ציון ב-{{.subject}}: {{.value}}!"}, e.now); err != nil {
		t.Fatal(err)
	}
	e.src.grades[e.kid.ID] = []mashov.Grade{{ID: 1, GradingEventID: 9, SubjectName: "Math", Rate: 80}}
	_, _ = e.svc.RunDue(ctx) // baseline
	e.now = e.now.Add(time.Hour)
	e.src.grades[e.kid.ID] = append(e.src.grades[e.kid.ID], mashov.Grade{ID: 2, GradingEventID: 9, SubjectName: "Art", Rate: 77})
	if _, err := e.svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	d := e.pending(t)
	if len(d) != 1 || d[0].Body != "ציון ב-Art: 77!" {
		t.Fatalf("body = %+v (want the override rendered)", d)
	}
}
