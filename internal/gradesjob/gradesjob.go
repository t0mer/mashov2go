// Package gradesjob syncs each child's grades and tells the family about new and
// changed grades (CLAUDE.md §5, §6.2). Grades are a parent category by default.
// The first sync of a child is a silent baseline, so existing grades are not
// announced. Later syncs queue one message per new or updated grade.
package gradesjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/notify"
	"github.com/t0mer/mashov2go/internal/routing"
	"github.com/t0mer/mashov2go/internal/schedule"
	"github.com/t0mer/mashov2go/internal/store"
	"github.com/t0mer/mashov2go/internal/syncer"
)

// DefaultGap is how often each child's grades are fetched when no setting says otherwise.
const DefaultGap = time.Hour

// Source fetches a child's grades for a window (implemented by mashovsource.Source).
type Source interface {
	Grades(ctx context.Context, child family.Member, start, end time.Time) ([]mashov.Grade, error)
}

// Service runs the grades sync for every linked child.
type Service struct {
	Store  *store.Store
	Source Source
	Syncer *syncer.Syncer
	Loc    *time.Location
	Now    func() time.Time
	// Gap returns the time between two fetches of one child; nil uses DefaultGap.
	Gap func() time.Duration

	lastRun map[int64]time.Time
}

// RunDue fetches the grades of every active linked child whose last fetch is older
// than the gap. It returns how many grade messages it queued. One child failing
// does not stop the others.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	now := s.now()
	if s.lastRun == nil {
		s.lastRun = map[int64]time.Time{}
	}
	members, err := s.Store.ListMembers(ctx)
	if err != nil {
		return 0, err
	}
	var parents []family.Member
	for _, m := range members {
		if m.Role == family.RoleParent && m.Active {
			parents = append(parents, m)
		}
	}
	queued := 0
	var errs []error
	for _, m := range members {
		if m.Role != family.RoleChild || !m.Active {
			continue
		}
		if last, ok := s.lastRun[m.ID]; ok && now.Sub(last) < s.gap() {
			continue
		}
		s.lastRun[m.ID] = now // set before fetching, so a failing child also waits
		n, err := s.syncChild(ctx, m, parents, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("child %d: %w", m.ID, err))
			continue
		}
		queued += n
	}
	return queued, errors.Join(errs...)
}

func (s *Service) syncChild(ctx context.Context, m family.Member, parents []family.Member, now time.Time) (int, error) {
	accountID, guid, linked, err := s.Store.MemberLink(ctx, m.ID)
	if err != nil {
		return 0, err
	}
	if !linked {
		return 0, nil
	}
	grades, err := s.Source.Grades(ctx, m, schoolYearStart(now), now.AddDate(0, 0, 1))
	if err != nil {
		return 0, err
	}
	raw := make([]json.RawMessage, 0, len(grades))
	for _, g := range grades {
		b, err := json.Marshal(g)
		if err != nil {
			return 0, err
		}
		raw = append(raw, b)
	}
	res, err := s.Syncer.Apply(ctx, accountID, guid, "grades", raw)
	if err != nil {
		return 0, err
	}
	if res.Baseline {
		return 0, nil
	}
	recipients, err := s.Store.Recipients(ctx, m.ID)
	if err != nil {
		return 0, err
	}
	muted := map[int64]bool{}
	for _, p := range parents {
		if ok, err := s.Store.IsMuted(ctx, p.ID, m.ID, family.CatGrades); err != nil {
			return 0, err
		} else if ok {
			muted[p.ID] = true
		}
	}
	queued := 0
	for _, ev := range res.Events {
		eventID, ok, err := s.Store.EventIDByDedupe(ctx, ev.DedupeKey)
		if err != nil {
			return queued, err
		}
		if !ok {
			return queued, fmt.Errorf("event %q missing after insert", ev.DedupeKey)
		}
		body, err := message(m.Name, ev.Kind, ev.Data)
		if err != nil {
			return queued, err
		}
		targets := routing.Route(routing.Input{Category: family.CatGrades, Child: m, Overrides: recipients, Parents: parents, Muted: muted})
		for _, t := range targets {
			if err := notify.Enqueue(ctx, s.Store, s.Loc, eventID, t, body, now); err != nil {
				return queued, err
			}
			queued++
		}
	}
	return queued, nil
}

// message renders a grade event for the family (CLAUDE.md §7 parent template).
func message(child, kind, payload string) (string, error) {
	var g mashov.Grade
	if err := json.Unmarshal([]byte(payload), &g); err != nil {
		return "", fmt.Errorf("grade payload: %w", err)
	}
	head := "📊 ציון חדש"
	if strings.HasSuffix(kind, "updated") {
		head = "📊 ציון עודכן"
	}
	value := g.RangeGrade
	if g.Rate > 0 {
		value = fmt.Sprint(g.Rate)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s – %s\n%s", head, child, g.SubjectName)
	if g.GradingEvent != "" {
		fmt.Fprintf(&b, " · %s", g.GradingEvent)
	}
	fmt.Fprintf(&b, "\nציון: %s", value)
	return b.String(), nil
}

// schoolYearStart is September 1 of the school year that contains now (CLAUDE.md §2).
func schoolYearStart(now time.Time) time.Time {
	year := schedule.SchoolYearEnd(now)
	return time.Date(year-1, time.September, 1, 0, 0, 0, 0, time.UTC)
}

func (s *Service) gap() time.Duration {
	if s.Gap != nil {
		return s.Gap()
	}
	return DefaultGap
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
