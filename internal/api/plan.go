package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/planner"
	"github.com/t0mer/mashov2go/internal/schedule"
)

type lessonView struct {
	Num       int    `json:"num"`
	Start     string `json:"start"`
	End       string `json:"end"`
	Subject   string `json:"subject"`
	Room      string `json:"room"`
	Note      string `json:"note"`
	Cancelled bool   `json:"cancelled"`
}

type planView struct {
	Date        string         `json:"date"`
	Skip        string         `json:"skip"`
	HolidayName string         `json:"holidayName"`
	Lessons     []lessonView   `json:"lessons"`
	Homework    []homeworkView `json:"homework"`
}

type homeworkView struct {
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// planRoutes registers the read-only plan endpoints for one child (CLAUDE.md §9).
func (s *Server) planRoutes(mux *http.ServeMux, base string) {
	mux.Handle("GET "+base+"/{id}/plan", s.authed("read", s.childPlan(false)))
	mux.Handle("GET "+base+"/{id}/plan/preview", s.authed("read", s.childPlan(true)))
}

// planDate returns the school day to plan: ?date=YYYY-MM-DD, or tomorrow in the
// configured timezone (CLAUDE.md §7).
func (s *Server) planDate(r *http.Request) (string, time.Time, error) {
	zone := "Asia/Jerusalem"
	if s.Settings != nil {
		zone = s.Settings.Get().Timezone
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return "", time.Time{}, err
	}
	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		dateStr = planner.TargetDate(s.now(), loc)
	}
	day, err := time.ParseInLocation("2006-01-02", dateStr, loc)
	if err != nil {
		return "", time.Time{}, errors.New("date must be YYYY-MM-DD")
	}
	return dateStr, day, nil
}

// childPlan returns the computed plan for a child, or its rendered text when preview is true.
func (s *Server) childPlan(preview bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := s.loadMember(w, r, family.RoleChild)
		if !ok {
			return
		}
		if s.Plans == nil {
			problem(w, http.StatusServiceUnavailable, "Plans unavailable", "the plan source is not configured")
			return
		}
		dateStr, day, err := s.planDate(r)
		if err != nil {
			problem(w, http.StatusBadRequest, "Bad request", err.Error())
			return
		}
		in, firstName, err := s.Plans.Load(r.Context(), m, day)
		if err != nil {
			problem(w, http.StatusBadGateway, "Mashov data unavailable", "the child's school data could not be loaded; try again later")
			return
		}
		p := schedule.Build(in)
		if preview {
			text := ""
			if p.Skip == "" {
				if text, err = schedule.Render("", schedule.ChildView{FirstName: firstName, Plan: p}); err != nil {
					problem(w, http.StatusInternalServerError, "Internal error", "rendering the plan failed")
					return
				}
			}
			writeJSON(w, http.StatusOK, map[string]string{"date": dateStr, "skip": p.Skip, "text": text})
			return
		}
		writeJSON(w, http.StatusOK, viewPlan(dateStr, p))
	})
}

func viewPlan(dateStr string, p schedule.Plan) planView {
	out := planView{Date: dateStr, Skip: p.Skip, HolidayName: p.HolidayName,
		Lessons: []lessonView{}, Homework: []homeworkView{}}
	for _, l := range p.Lessons {
		out.Lessons = append(out.Lessons, lessonView{Num: l.Num, Start: l.Start, End: l.End,
			Subject: l.Subject, Room: l.Room, Note: l.Note, Cancelled: l.Cancelled})
	}
	for _, h := range p.Homework {
		out.Homework = append(out.Homework, homeworkView{Subject: h.Subject, Text: h.Text})
	}
	return out
}
