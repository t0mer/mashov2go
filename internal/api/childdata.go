package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/mashovsource"
)

// ChildData reads a child's school data from Mashov for the child pages. It is
// implemented by mashovsource.Source. Only the shapes confirmed against live
// Mashov are exposed here (CLAUDE.md §4.1).
type ChildData interface {
	Timetable(ctx context.Context, child family.Member) ([]mashov.Lesson, error)
	Homework(ctx context.Context, child family.Member) ([]mashov.HomeworkItem, error)
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
