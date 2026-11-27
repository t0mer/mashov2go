package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/mashovsource"
	"github.com/t0mer/mashov2go/internal/schedule"
)

// ChildData reads a child's school data from Mashov for the child pages. It is
// implemented by mashovsource.Source. Only the shapes confirmed against live
// Mashov are exposed here (CLAUDE.md §4.1).
type ChildData interface {
	Timetable(ctx context.Context, child family.Member) ([]mashov.Lesson, error)
	Homework(ctx context.Context, child family.Member) ([]mashov.HomeworkItem, error)
	Grades(ctx context.Context, child family.Member, start, end time.Time) ([]mashov.Grade, error)
	Behave(ctx context.Context, child family.Member, start, end time.Time) ([]mashov.Behave, error)
}

type gradeView struct {
	ID            int    `json:"id"`
	Subject       string `json:"subject"`
	Event         string `json:"event"`
	GradeType     string `json:"gradeType"`
	Rate          int    `json:"rate"`
	RangeGrade    string `json:"rangeGrade"`
	EventDate     string `json:"eventDate"`
	GradingPeriod int    `json:"gradingPeriod"`
}

type behaveView struct {
	LessonID      int    `json:"lessonId"`
	Subject       string `json:"subject"`
	Lesson        int    `json:"lesson"`
	LessonDate    string `json:"lessonDate"`
	Name          string `json:"name"`
	Justification string `json:"justification"`
}

type timetableView struct {
	Day     int    `json:"day"`
	Lesson  int    `json:"lesson"`
	GroupID int    `json:"groupId"`
	Subject string `json:"subject"`
	Room    string `json:"room"`
	Weeks   int    `json:"weeks"`
}

type homeworkItemView struct {
	LessonDate string `json:"lessonDate"`
	Lesson     int    `json:"lesson"`
	Subject    string `json:"subject"`
	Homework   string `json:"homework"`
	Remark     string `json:"remark"`
}

func (s *Server) childDataRoutes(mux *http.ServeMux, base string) {
	s.handle(mux, "GET "+base+"/{id}/timetable", s.authed("read", s.childTimetable()))
	s.handle(mux, "GET "+base+"/{id}/homework", s.authed("read", s.childHomework()))
	s.handle(mux, "GET "+base+"/{id}/grades", s.authed("read", s.childGrades()))
	s.handle(mux, "GET "+base+"/{id}/behave", s.authed("read", s.childBehave()))
}

// childDataError maps a Mashov read failure to a response.
func childDataError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mashovsource.ErrNotLinked):
		problem(w, http.StatusConflict, "Not linked", "this child is not linked to a Mashov student")
	case errors.Is(err, mashovsource.ErrPasswordCooldown):
		problem(w, http.StatusBadGateway, "Mashov sign-in paused", "the account must wait before signing in again")
	default:
		problem(w, http.StatusBadGateway, "Mashov unavailable", "the school data could not be loaded; try again later")
	}
}

func (s *Server) childTimetable() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		if s.ChildData == nil {
			problem(w, http.StatusServiceUnavailable, "Data unavailable", "the school data source is not configured")
			return
		}
		lessons, err := s.ChildData.Timetable(r.Context(), m)
		if err != nil {
			childDataError(w, err)
			return
		}
		out := make([]timetableView, 0, len(lessons))
		for _, l := range lessons {
			out = append(out, timetableView{
				Day: l.TimeTable.Day, Lesson: l.TimeTable.Lesson, GroupID: l.TimeTable.GroupID,
				Subject: l.GroupDetails.SubjectName, Room: l.TimeTable.RoomNum, Weeks: l.TimeTable.Weeks,
			})
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func (s *Server) childHomework() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		if s.ChildData == nil {
			problem(w, http.StatusServiceUnavailable, "Data unavailable", "the school data source is not configured")
			return
		}
		items, err := s.ChildData.Homework(r.Context(), m)
		if err != nil {
			childDataError(w, err)
			return
		}
		out := make([]homeworkItemView, 0, len(items))
		for _, h := range items {
			out = append(out, homeworkItemView{LessonDate: h.LessonDate, Lesson: h.Lesson,
				Subject: h.SubjectName, Homework: h.Homework, Remark: h.Remark})
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// windowParam reads ?start= and ?end= (YYYY-MM-DD, school time). Without them the
// window is the current school year, as the sync uses (CLAUDE.md §2).
func windowParam(r *http.Request, now time.Time) (time.Time, time.Time, error) {
	start := time.Date(schedule.SchoolYearEnd(now)-1, time.September, 1, 0, 0, 0, 0, time.UTC)
	end := now.AddDate(0, 0, 1)
	if v := r.URL.Query().Get("start"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return start, end, errors.New("start must be YYYY-MM-DD")
		}
		start = t
	}
	if v := r.URL.Query().Get("end"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return start, end, errors.New("end must be YYYY-MM-DD")
		}
		end = t
	}
	return start, end, nil
}

func (s *Server) childGrades() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		if s.ChildData == nil {
			problem(w, http.StatusServiceUnavailable, "Data unavailable", "the school data source is not configured")
			return
		}
		from, to, err := windowParam(r, s.now())
		if err != nil {
			problem(w, http.StatusBadRequest, "Bad request", err.Error())
			return
		}
		items, err := s.ChildData.Grades(r.Context(), m, from, to)
		if err != nil {
			childDataError(w, err)
			return
		}
		out := make([]gradeView, 0, len(items))
		for _, g := range items {
			out = append(out, gradeView{ID: g.ID, Subject: g.SubjectName, Event: g.GradingEvent, GradeType: g.GradeType,
				Rate: g.Rate, RangeGrade: g.RangeGrade, EventDate: g.EventDate, GradingPeriod: g.GradingPeriod})
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func (s *Server) childBehave() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		if s.ChildData == nil {
			problem(w, http.StatusServiceUnavailable, "Data unavailable", "the school data source is not configured")
			return
		}
		from, to, err := windowParam(r, s.now())
		if err != nil {
			problem(w, http.StatusBadRequest, "Bad request", err.Error())
			return
		}
		items, err := s.ChildData.Behave(r.Context(), m, from, to)
		if err != nil {
			childDataError(w, err)
			return
		}
		out := make([]behaveView, 0, len(items))
		for _, b := range items {
			out = append(out, behaveView{LessonID: b.LessonID, Subject: b.Subject, Lesson: b.Lesson,
				LessonDate: b.LessonDate, Name: b.AchvaName, Justification: b.Justification})
		}
		writeJSON(w, http.StatusOK, out)
	})
}
