package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

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

func (f fakeChildData) Grades(_ context.Context, _ family.Member, from, to time.Time) ([]mashov.Grade, error) {
	if f.err != nil {
		return nil, f.err
	}
	if to.Before(from) {
		return nil, errors.New("bad window")
	}
	return []mashov.Grade{{ID: 3, SubjectName: "Math", GradingEvent: "Quiz", Rate: 92, GradingPeriod: 1}}, nil
}

func (f fakeChildData) Behave(context.Context, family.Member, time.Time, time.Time) ([]mashov.Behave, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []mashov.Behave{{LessonID: 4, Subject: "Math", Lesson: 2, AchvaName: "איחור", Justification: "bus"}}, nil
}

func TestChildGradesAndBehave(t *testing.T) {
	srv, h := newServer(t)
	srv.ChildData = fakeChildData{}
	cookie, csrf := signIn(t, h)
	id := kidID(t, h, cookie, csrf)

	g := call(h, http.MethodGet, "/api/v1/children/"+id+"/grades?start=2026-09-01&end=2026-11-27", nil, cookie, nil)
	if g.Code != http.StatusOK || !strings.Contains(g.Body.String(), `"rate":92`) || !strings.Contains(g.Body.String(), `"subject":"Math"`) {
		t.Fatalf("grades: %d %s", g.Code, g.Body.String())
	}
	b := call(h, http.MethodGet, "/api/v1/children/"+id+"/behave", nil, cookie, nil)
	if b.Code != http.StatusOK || !strings.Contains(b.Body.String(), `"name":"איחור"`) || !strings.Contains(b.Body.String(), `"justification":"bus"`) {
		t.Fatalf("behave: %d %s", b.Code, b.Body.String())
	}
	if bad := call(h, http.MethodGet, "/api/v1/children/"+id+"/grades?start=1-9-2026", nil, cookie, nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("bad start: %d, want 400", bad.Code)
	}
}

func TestChildGradesAndBehave_NoSource(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	id := kidID(t, h, cookie, csrf)
	if rec := call(h, http.MethodGet, "/api/v1/children/"+id+"/grades", nil, cookie, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no source: %d, want 503", rec.Code)
	}
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
