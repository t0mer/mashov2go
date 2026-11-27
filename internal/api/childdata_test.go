package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/mashovsource"
)

type fakeChildData struct{ err error }

func (f fakeChildData) Timetable(context.Context, family.Member) ([]mashov.Lesson, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []mashov.Lesson{{
		TimeTable:    mashov.TimeTableSlot{Day: 6, Lesson: 1, GroupID: 10, RoomNum: "12", Weeks: -1},
		GroupDetails: mashov.GroupDetails{GroupID: 10, SubjectName: "Math"},
	}}, nil
}

func (f fakeChildData) Homework(context.Context, family.Member) ([]mashov.HomeworkItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []mashov.HomeworkItem{{SubjectName: "Math", Homework: "pages 4-5", LessonDate: "2026-11-26T00:00:00", Lesson: 2}}, nil
}

func TestChildTimetableAndHomework(t *testing.T) {
	srv, h := newServer(t)
	srv.ChildData = fakeChildData{}
	cookie, csrf := signIn(t, h)
	id := kidID(t, h, cookie, csrf)

	tt := call(h, http.MethodGet, "/api/v1/children/"+id+"/timetable", nil, cookie, nil)
	if tt.Code != http.StatusOK || !strings.Contains(tt.Body.String(), `"subject":"Math"`) || !strings.Contains(tt.Body.String(), `"room":"12"`) {
		t.Fatalf("timetable: %d %s", tt.Code, tt.Body.String())
	}
	hw := call(h, http.MethodGet, "/api/v1/children/"+id+"/homework", nil, cookie, nil)
	if hw.Code != http.StatusOK || !strings.Contains(hw.Body.String(), `"homework":"pages 4-5"`) {
		t.Fatalf("homework: %d %s", hw.Code, hw.Body.String())
	}
}

func TestChildDataErrors(t *testing.T) {
	srv, h := newServer(t)
	cookie, csrf := signIn(t, h)
	id := kidID(t, h, cookie, csrf)

	if rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/timetable", nil, cookie, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no source: %d, want 503", rec.Code)
	}
	srv.ChildData = fakeChildData{err: mashovsource.ErrNotLinked}
	if rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/homework", nil, cookie, nil); rec.Code != http.StatusConflict {
		t.Fatalf("unlinked: %d, want 409", rec.Code)
	}
	srv.ChildData = fakeChildData{err: http.ErrServerClosed}
	if rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/timetable", nil, cookie, nil); rec.Code != http.StatusBadGateway {
		t.Fatalf("mashov failure: %d, want 502", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/timetable", nil, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d, want 401", rec.Code)
	}
}
