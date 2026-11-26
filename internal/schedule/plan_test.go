package schedule

import (
	"strings"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/mashov"
)

// Monday 2026-11-30: week 14 of school year 2027, weekday Monday = day 2.
var monday = time.Date(2026, time.November, 30, 0, 0, 0, 0, time.UTC)

func slot(day, lesson, group int, subject string, weeks int) mashov.Lesson {
	return mashov.Lesson{
		TimeTable:    mashov.TimeTableSlot{Day: day, Lesson: lesson, GroupID: group, RoomNum: "12", Weeks: weeks},
		GroupDetails: mashov.GroupDetails{GroupID: group, SubjectName: subject},
	}
}

var bells = []mashov.Bell{
	{LessonNumber: 1, StartTime: "08:00:00", EndTime: "08:45:00"},
	{LessonNumber: 2, StartTime: "08:50:00", EndTime: "09:35:00"},
	{LessonNumber: 3, StartTime: "09:40:00", EndTime: "10:25:00"},
}

func base() Input {
	return Input{
		Date: monday,
		Lessons: []mashov.Lesson{
			slot(2, 2, 10, "Math", 0),
			slot(2, 1, 11, "English", 0),
			slot(2, 3, 12, "Art", 0),
		},
		Bells: bells,
	}
}

func TestNormalDayIsSortedWithBellTimes(t *testing.T) {
	p := Build(base())
	if p.Skip != "" {
		t.Fatalf("unexpected skip %q", p.Skip)
	}
	if len(p.Lessons) != 3 {
		t.Fatalf("lessons = %d, want 3", len(p.Lessons))
	}
	want := []struct {
		num         int
		start, subj string
	}{
		{1, "08:00", "English"}, {2, "08:50", "Math"}, {3, "09:40", "Art"},
	}
	for i, w := range want {
		l := p.Lessons[i]
		if l.Num != w.num || l.Start != w.start || l.Subject != w.subj {
			t.Errorf("lesson %d = %+v, want %+v", i, l, w)
		}
	}
}

func TestHolidaySkips(t *testing.T) {
	in := base()
	in.Holidays = []mashov.Holiday{{Name: "Hanukkah", StartDate: "2026-11-29T00:00:00", EndDate: "2026-12-01T00:00:00"}}
	p := Build(in)
	if p.Skip != SkipHoliday || p.HolidayName != "Hanukkah" {
		t.Fatalf("skip=%q holiday=%q", p.Skip, p.HolidayName)
	}
}

func TestWeekdayWithoutLessonsSkips(t *testing.T) {
	in := base()
	in.Date = monday.AddDate(0, 0, 4) // Friday
	p := Build(in)
	if p.Skip != SkipNoLessons {
		t.Fatalf("skip=%q, want %q", p.Skip, SkipNoLessons)
	}
}

func TestAlternatingWeeks(t *testing.T) {
	in := base()
	// Week 14 is this week. Bit 13 only (not 14) means the lesson does not run.
	in.Lessons = []mashov.Lesson{
		slot(2, 1, 11, "English", 0),
		slot(2, 2, 10, "Math", 1<<13),
		slot(2, 3, 12, "Art", 1<<14),
	}
	p := Build(in)
	subjects := []string{}
	for _, l := range p.Lessons {
		subjects = append(subjects, l.Subject)
	}
	if got := strings.Join(subjects, ","); got != "English,Art" {
		t.Fatalf("subjects = %s, want English,Art", got)
	}
}

func TestCancelFreeAndSubstitute(t *testing.T) {
	in := base()
	in.Changes = &mashov.TimetableChanges{
		Changes: []mashov.ChangeItem{
			{GroupID: 10, LessonDate: "2026-11-30T00:00:00", Lesson: 2, Action: mashov.ActionCancel},
			{GroupID: 11, LessonDate: "2026-11-30T00:00:00", Lesson: 1, Action: mashov.ActionSubstitute},
		},
		Cancellations: []mashov.ChangeItem{
			{GroupID: 12, LessonDate: "2026-11-30T00:00:00", Lesson: 3, Action: mashov.ActionFree},
		},
	}
	p := Build(in)
	if p.Skip != "" {
		t.Fatalf("unexpected skip %q", p.Skip)
	}
	if !p.Lessons[1].Cancelled || p.Lessons[1].Note != NoteCancelled {
		t.Errorf("math not cancelled: %+v", p.Lessons[1])
	}
	if p.Lessons[0].Note != NoteSubstitute || p.Lessons[0].Cancelled {
		t.Errorf("english substitute wrong: %+v", p.Lessons[0])
	}
	if !p.Lessons[2].Cancelled || p.Lessons[2].Note != NoteFree {
		t.Errorf("art free wrong: %+v", p.Lessons[2])
	}
}

func TestOnlineAndAddition(t *testing.T) {
	in := base()
	in.Changes = &mashov.TimetableChanges{Changes: []mashov.ChangeItem{
		{GroupID: 11, LessonDate: "2026-11-30T00:00:00", Lesson: 1, Action: mashov.ActionOnline},
		// Addition for group 10 at lesson 5 (no existing slot): subject comes from the timetable.
		{GroupID: 10, LessonDate: "2026-11-30T00:00:00", Lesson: 5, Action: mashov.ActionAddition},
	}}
	p := Build(in)
	if p.Lessons[0].Note != NoteOnline {
		t.Errorf("online note = %q", p.Lessons[0].Note)
	}
	last := p.Lessons[len(p.Lessons)-1]
	if last.Num != 5 || last.Subject != "Math" || last.Note != NoteAdded {
		t.Errorf("addition = %+v", last)
	}
}

func TestChangesForOtherDaysAreIgnored(t *testing.T) {
	in := base()
	in.Changes = &mashov.TimetableChanges{Changes: []mashov.ChangeItem{
		{GroupID: 10, LessonDate: "2026-12-01T00:00:00", Lesson: 2, Action: mashov.ActionCancel},
	}}
	p := Build(in)
	for _, l := range p.Lessons {
		if l.Cancelled || l.Note != "" {
			t.Fatalf("change for another day leaked into plan: %+v", l)
		}
	}
}

func TestAllCancelledSkips(t *testing.T) {
	in := base()
	in.Changes = &mashov.TimetableChanges{Cancellations: []mashov.ChangeItem{
		{GroupID: 10, LessonDate: "2026-11-30T00:00:00", Lesson: 2, Action: mashov.ActionCancel},
		{GroupID: 11, LessonDate: "2026-11-30T00:00:00", Lesson: 1, Action: mashov.ActionCancel},
		{GroupID: 12, LessonDate: "2026-11-30T00:00:00", Lesson: 3, Action: mashov.ActionCancel},
	}}
	if p := Build(in); p.Skip != SkipAllCancelled {
		t.Fatalf("skip=%q, want %q", p.Skip, SkipAllCancelled)
	}
}

func TestHomeworkGivenTodayOnlyWhenEnabled(t *testing.T) {
	in := base()
	in.Homework = []mashov.HomeworkItem{
		{SubjectName: "Math", Homework: "pages 7-12", LessonDate: "2026-11-29T00:00:00"},
		{SubjectName: "Art", Homework: "draw", LessonDate: "2026-11-25T00:00:00"},
	}
	if got := Build(in).Homework; len(got) != 0 {
		t.Fatalf("homework included while disabled: %+v", got)
	}
	in.IncludeHomeworkGivenToday = true
	got := Build(in).Homework
	if len(got) != 1 || got[0].Subject != "Math" || got[0].Text != "pages 7-12" {
		t.Fatalf("homework = %+v", got)
	}
}

// Golden: the default Hebrew template, rendered for a plan with a cancellation
// and one homework line.
func TestRenderGolden(t *testing.T) {
	in := base()
	in.Changes = &mashov.TimetableChanges{Changes: []mashov.ChangeItem{
		{GroupID: 10, LessonDate: "2026-11-30T00:00:00", Lesson: 2, Action: mashov.ActionCancel},
	}}
	in.IncludeHomeworkGivenToday = true
	in.Homework = []mashov.HomeworkItem{{SubjectName: "Art", Homework: "draw a sketch", LessonDate: "2026-11-29T00:00:00"}}
	out, err := Render("", ChildView{FirstName: "Test", Plan: Build(in)})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"🎒 Test, המערכת למחר – יום שני 30/11",
		"",
		"1. 08:00–08:45  English · חדר 12",
		"2. 08:50–09:35  Math · חדר 12  ❌ בוטל",
		"3. 09:40–10:25  Art · חדר 12",
		"",
		"📚 שיעורי בית שניתנו היום:",
		"• Art: draw a sketch",
		"",
		"לילה טוב 🌙",
	}, "\n")
	if out != want {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}
