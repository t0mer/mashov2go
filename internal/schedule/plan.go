package schedule

import (
	"sort"
	"time"

	"github.com/t0mer/mashov2go/internal/mashov"
)

// Skip reasons. An empty Plan.Skip means the plan should be sent.
const (
	SkipHoliday      = "holiday"
	SkipNoLessons    = "no_lessons"
	SkipAllCancelled = "all_cancelled"
)

// Notes shown next to a lesson (CLAUDE.md §7).
const (
	NoteCancelled  = "❌ בוטל"
	NoteFree       = "שיעור חופשי"
	NoteSubstitute = "🔁 מילוי מקום"
	NoteOnline     = "💻 מקוון"
	NoteAdded      = "➕ נוסף"
	NoteMoved      = "🔁 הועבר"
)

// Input is everything needed to build one child's plan for Date.
type Input struct {
	Date     time.Time // the target school day D (civil date)
	Lessons  []mashov.Lesson
	Changes  *mashov.TimetableChanges
	Bells    []mashov.Bell
	Holidays []mashov.Holiday
	Homework []mashov.HomeworkItem
	// IncludeHomeworkGivenToday adds homework whose lessonDate is D-1.
	IncludeHomeworkGivenToday bool
}

// Lesson is one rendered row of the plan.
type Lesson struct {
	Num       int
	Start     string
	End       string
	Subject   string
	Room      string
	Note      string
	Cancelled bool

	groupID int // timetable group, used to match change items
}

// Homework is one line of "homework given today".
type Homework struct {
	Subject string
	Text    string
}

// Plan is the computed plan for one school day.
type Plan struct {
	Date        time.Time
	Skip        string // SkipHoliday, SkipNoLessons, SkipAllCancelled, or ""
	HolidayName string
	Lessons     []Lesson
	Homework    []Homework
}

// Build computes the plan for in.Date. It is pure: no I/O, no clock.
func Build(in Input) Plan {
	day := civil(in.Date)
	p := Plan{Date: day}

	if name, ok := holidayOn(in.Holidays, day); ok {
		p.Skip, p.HolidayName = SkipHoliday, name
		return p
	}

	week := SchoolWeek(day, SchoolYearEnd(day))
	dow := int(day.Weekday()) + 1 // Sunday=1 … Saturday=7

	groupSubject := map[int]string{}
	for _, l := range in.Lessons {
		groupSubject[l.GroupDetails.GroupID] = l.GroupDetails.SubjectName
	}

	bells := map[int]mashov.Bell{}
	for _, b := range in.Bells {
		bells[b.LessonNumber] = b
	}

	// Base slots: timetable entries for this weekday that run this week.
	var lessons []Lesson
	for _, l := range in.Lessons {
		if l.TimeTable.Day != dow || !LessonRunsInWeek(l.TimeTable.Weeks, week) {
			continue
		}
		lessons = append(lessons, Lesson{
			Num:     l.TimeTable.Lesson,
			Subject: l.GroupDetails.SubjectName,
			Room:    l.TimeTable.RoomNum,
			groupID: l.TimeTable.GroupID,
		})
	}
	if len(lessons) == 0 {
		p.Skip = SkipNoLessons
		return p
	}

	if in.Changes != nil {
		changes := append(append([]mashov.ChangeItem{}, in.Changes.Changes...), in.Changes.Cancellations...)
		for _, ch := range changes {
			if !sameDay(ch.LessonDate, day) {
				continue
			}
			lessons = applyChange(lessons, ch, groupSubject)
		}
	}

	for i := range lessons {
		if b, ok := bells[lessons[i].Num]; ok {
			lessons[i].Start, lessons[i].End = trimSeconds(b.StartTime), trimSeconds(b.EndTime)
		}
	}
	sort.SliceStable(lessons, func(i, j int) bool { return lessons[i].Num < lessons[j].Num })

	allCancelled := true
	for _, l := range lessons {
		if !l.Cancelled {
			allCancelled = false
			break
		}
	}
	if allCancelled {
		p.Skip = SkipAllCancelled
		p.Lessons = lessons
		return p
	}
	p.Lessons = lessons

	if in.IncludeHomeworkGivenToday {
		yesterday := day.AddDate(0, 0, -1)
		for _, h := range in.Homework {
			if sameDay(h.LessonDate, yesterday) {
				p.Homework = append(p.Homework, Homework{Subject: h.SubjectName, Text: h.Homework})
			}
		}
	}
	return p
}

// applyChange updates lessons for one change item, matching on group and lesson number.
func applyChange(lessons []Lesson, ch mashov.ChangeItem, groupSubject map[int]string) []Lesson {
	idx := -1
	for i, l := range lessons {
		if l.Num == ch.Lesson && l.groupID == ch.GroupID {
			idx = i
			break
		}
	}
	switch ch.Action {
	case mashov.ActionCancel:
		if idx >= 0 {
			lessons[idx].Cancelled, lessons[idx].Note = true, NoteCancelled
		}
	case mashov.ActionFree:
		if idx >= 0 {
			lessons[idx].Cancelled, lessons[idx].Note = true, NoteFree
		}
	case mashov.ActionSubstitute:
		if idx >= 0 {
			lessons[idx].Note = NoteSubstitute
		}
	case mashov.ActionOnline:
		if idx >= 0 {
			lessons[idx].Note = NoteOnline
		}
	case mashov.ActionMove:
		// The destination slot fields are not confirmed yet (CLAUDE.md §15), so a
		// move only annotates the slot it matches and never relocates a lesson.
		if idx >= 0 {
			lessons[idx].Note = NoteMoved
		}
	case mashov.ActionAddition, mashov.ActionExtra:
		if idx < 0 {
			if subj, ok := groupSubject[ch.GroupID]; ok {
				lessons = append(lessons, Lesson{Num: ch.Lesson, Subject: subj, Note: NoteAdded, groupID: ch.GroupID})
			}
		}
	}
	return lessons
}

func holidayOn(holidays []mashov.Holiday, day time.Time) (string, bool) {
	for _, h := range holidays {
		start, ok1 := parseDay(h.StartDate)
		end, ok2 := parseDay(h.EndDate)
		if !ok1 || !ok2 {
			continue
		}
		if !day.Before(start) && !day.After(end) {
			return h.Name, true
		}
	}
	return "", false
}

func sameDay(ts string, day time.Time) bool {
	t, ok := parseDay(ts)
	return ok && t.Equal(day)
}

// trimSeconds turns "07:57:00" into "07:57".
func trimSeconds(s string) string {
	if len(s) >= 5 {
		return s[:5]
	}
	return s
}
