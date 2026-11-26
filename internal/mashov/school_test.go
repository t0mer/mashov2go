package mashov

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Synthetic bodies that mirror the confirmed field names (CLAUDE.md §4.1).
// They contain no real school data.
const (
	synthTimetable = `[{"timeTable":{"day":2,"groupId":10,"lesson":1,"roomNum":"12","weeks":0},
	  "groupDetails":{"groupId":10,"groupName":"G1","subjectName":"Math","groupTeachers":"[redacted]"}}]`
	synthChanges  = `{"isEnabled":true,"changes":[{"groupId":10,"lessonDate":"2026-11-30T00:00:00","lesson":1,"action":2}],"cancellations":[]}`
	synthBells    = `[{"lessonNumber":1,"startTime":"08:00:00","endTime":"08:45:00"}]`
	synthHolidays = `[{"id":1,"hollyDayName":"Test","startDate":"2026-08-15T00:00:00","endDate":"2026-08-31T00:00:00"}]`
	synthHomework = `[{"groupId":10,"homework":"pages 1-2","lesson":1,"lessonDate":"2026-11-29T00:00:00","lessonId":7,"remark":"","subjectName":"Math"}]`
)

func TestTypedDecodersReadConfirmedFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/timetable/changes"):
			if r.URL.Query().Get("start") == "" || r.URL.Query().Get("end") == "" {
				t.Errorf("changes request missing range: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(synthChanges))
		case strings.HasSuffix(r.URL.Path, "/timetable"):
			_, _ = w.Write([]byte(synthTimetable))
		case strings.HasSuffix(r.URL.Path, "/homework"):
			_, _ = w.Write([]byte(synthHomework))
		case strings.HasSuffix(r.URL.Path, "/bells"):
			_, _ = w.Write([]byte(synthBells))
		case strings.HasSuffix(r.URL.Path, "/holidays"):
			_, _ = w.Write([]byte(synthHolidays))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()

	lessons, err := c.Timetable(ctx, "sg")
	if err != nil || len(lessons) != 1 || lessons[0].TimeTable.Day != 2 || lessons[0].GroupDetails.SubjectName != "Math" {
		t.Fatalf("timetable = %+v, err %v", lessons, err)
	}
	ch, err := c.TimetableChangesFor(ctx, "sg", time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(ch.Changes) != 1 || ch.Changes[0].Action != ActionCancel {
		t.Fatalf("changes = %+v, err %v", ch, err)
	}
	bells, err := c.Bells(ctx)
	if err != nil || bells[0].StartTime != "08:00:00" {
		t.Fatalf("bells = %+v, err %v", bells, err)
	}
	holidays, err := c.Holidays(ctx)
	if err != nil || holidays[0].Name != "Test" {
		t.Fatalf("holidays = %+v, err %v", holidays, err)
	}
	hw, err := c.Homework(ctx, "sg")
	if err != nil || hw[0].Homework != "pages 1-2" || hw[0].LessonDate != "2026-11-29T00:00:00" {
		t.Fatalf("homework = %+v, err %v", hw, err)
	}
}
