package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/schedule"
)

// fakePlanSource returns fixed school data for any day.
type fakePlanSource struct {
	holidays []mashov.Holiday
}

func (f fakePlanSource) Load(_ context.Context, _ family.Member, day time.Time) (schedule.Input, string, error) {
	return schedule.Input{
		Date: day,
		Lessons: []mashov.Lesson{
			{TimeTable: mashov.TimeTableSlot{Day: 6, Lesson: 1, GroupID: 10}, GroupDetails: mashov.GroupDetails{GroupID: 10, SubjectName: "Math"}},
			{TimeTable: mashov.TimeTableSlot{Day: 6, Lesson: 2, GroupID: 20}, GroupDetails: mashov.GroupDetails{GroupID: 20, SubjectName: "English"}},
		},
		Changes:  &mashov.TimetableChanges{},
		Bells:    []mashov.Bell{{LessonNumber: 1, StartTime: "08:00:00", EndTime: "08:45:00"}, {LessonNumber: 2, StartTime: "08:50:00", EndTime: "09:35:00"}},
		Holidays: f.holidays,
	}, "Noa", nil
}

func kidID(t *testing.T, h http.Handler, cookie *http.Cookie, csrf string) string {
	t.Helper()
	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children",
		map[string]string{"name": "Noa Levi", "whatsappPhone": "0501234567"})
	return itoa(int64(decodeMap(t, rec.Body.String())["id"].(float64)))
}

func TestChildPlanAndPreview(t *testing.T) {
	srv, h := newServer(t)
	srv.Plans = fakePlanSource{}
	cookie, csrf := signIn(t, h)
	id := kidID(t, h, cookie, csrf)

	rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/plan?date=2026-11-27", nil, cookie, nil)
	got := decodeMap(t, rec.Body.String())
	lessons, _ := got["lessons"].([]any)
	if rec.Code != http.StatusOK || got["date"] != "2026-11-27" || len(lessons) != 2 || got["skip"] != "" {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body.String())
	}

	pv := call(h, http.MethodGet, "/api/v1/children/"+id+"/plan/preview?date=2026-11-27", nil, cookie, nil)
	if pv.Code != http.StatusOK || !strings.Contains(pv.Body.String(), "Math") || !strings.Contains(pv.Body.String(), "Noa") {
		t.Fatalf("preview: %d %s", pv.Code, pv.Body.String())
	}
}

func TestPlanSkipsHolidays(t *testing.T) {
	srv, h := newServer(t)
	srv.Plans = fakePlanSource{holidays: []mashov.Holiday{{ID: 1, Name: "Chanukah", StartDate: "2026-11-26T00:00:00", EndDate: "2026-11-28T00:00:00"}}}
	cookie, csrf := signIn(t, h)
	id := kidID(t, h, cookie, csrf)

	rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/plan?date=2026-11-27", nil, cookie, nil)
	got := decodeMap(t, rec.Body.String())
	if got["skip"] == "" || got["holidayName"] != "Chanukah" {
		t.Fatalf("holiday not skipped: %v", got)
	}
	pv := decodeMap(t, call(h, http.MethodGet, "/api/v1/children/"+id+"/plan/preview?date=2026-11-27", nil, cookie, nil).Body.String())
	if pv["text"] != "" {
		t.Fatalf("preview on a skipped day: %v", pv)
	}
}

func TestPlanErrors(t *testing.T) {
	srv, h := newServer(t)
	cookie, csrf := signIn(t, h)
	id := kidID(t, h, cookie, csrf)

	if rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/plan", nil, cookie, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no plan source: %d, want 503", rec.Code)
	}
	srv.Plans = fakePlanSource{}
	if rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/plan?date=27-11-2026", nil, cookie, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad date: %d, want 400", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/children/999/plan", nil, cookie, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown child: %d, want 404", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/plan", nil, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous plan: %d, want 401", rec.Code)
	}
}
