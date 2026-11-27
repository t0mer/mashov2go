// Package planjob runs the daily plan for every child whose plan is due: it
// loads the child's data, builds and renders the plan, routes it to the family
// (CLAUDE.md §6.3, §7) and queues the deliveries. It is idempotent: each plan
// version is one event, and each (event, member) pair is queued once.
package planjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/metrics"
	"github.com/t0mer/mashov2go/internal/notify"
	"github.com/t0mer/mashov2go/internal/planner"
	"github.com/t0mer/mashov2go/internal/routing"
	"github.com/t0mer/mashov2go/internal/schedule"
	"github.com/t0mer/mashov2go/internal/store"
)

// Category is the routing category of the daily plan.
const Category = "daily_plan"

// skipMarker is stored as the hash of a day with no school, so the job does not
// reload Mashov data for it on every tick.
const skipMarker = "skip"

// Source loads the Mashov data a plan needs for one child on the target day.
// The implementation owns the Mashov sessions; this package never calls Mashov.
type Source interface {
	Load(ctx context.Context, child family.Member, day time.Time) (schedule.Input, string, error)
}

// DefaultLoadGap is the minimum time between two Mashov loads for one child.
// Once a plan is due it stays due all evening, so without this the job would
// reload the child's data on every tick.
const DefaultLoadGap = 30 * time.Minute

// Service runs due plans.
type Service struct {
	Store  *store.Store
	Source Source
	Loc    *time.Location
	Now    func() time.Time
	// LoadGap overrides DefaultLoadGap when non-zero.
	LoadGap time.Duration
	// PlansSent counts plans recorded as sent; Events counts new events by
	// category. Both are optional (nil disables counting).
	PlansSent *metrics.Counter
	Events    *metrics.Counter

	lastLoad map[int64]time.Time
	cache    map[int64]fetched
}

// fetched is the Mashov data for one child and target date, kept from the
// prefetch (15 min before plan time) until the gap has passed.
type fetched struct {
	date string
	in   schedule.Input
	name string
	at   time.Time
}

// RunDue checks every active child with plans enabled and processes those that
// are due. It returns how many children had a plan queued or updated. A child
// that fails does not stop the others; their errors are joined in the result.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	now := s.now()
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
	done := 0
	var errs []error
	for _, m := range members {
		if m.Role != family.RoleChild || !m.Active {
			continue
		}
		due, err := planner.PlanDue(m, now, s.Loc)
		if err != nil {
			errs = append(errs, fmt.Errorf("child %d: %w", m.ID, err))
			continue
		}
		if !due {
			pre, err := planner.PrefetchDue(m, now, s.Loc)
			if err != nil {
				errs = append(errs, fmt.Errorf("child %d: %w", m.ID, err))
				continue
			}
			if pre {
				if err := s.prefetch(ctx, m, now); err != nil {
					errs = append(errs, fmt.Errorf("child %d prefetch: %w", m.ID, err))
				}
			}
			continue
		}
		changed, err := s.processChild(ctx, m, parents, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("child %d: %w", m.ID, err))
			continue
		}
		if changed {
			done++
		}
	}
	return done, errors.Join(errs...)
}

func (s *Service) gap() time.Duration {
	if s.LoadGap != 0 {
		return s.LoadGap
	}
	return DefaultLoadGap
}

func (s *Service) initMaps() {
	if s.lastLoad == nil {
		s.lastLoad = map[int64]time.Time{}
		s.cache = map[int64]fetched{}
	}
}

// throttled reports whether the child was loaded too recently, and records the
// load time when it was not. The time is recorded before loading, so a failing
// child also waits out the gap instead of being retried on every tick.
func (s *Service) throttled(id int64, now time.Time) bool {
	s.initMaps()
	if last, ok := s.lastLoad[id]; ok && now.Sub(last) < s.gap() {
		return true
	}
	s.lastLoad[id] = now
	return false
}

// fresh returns the cached data for the child and date when it is recent enough.
func (s *Service) fresh(id int64, dateStr string, now time.Time) (fetched, bool) {
	s.initMaps()
	c, ok := s.cache[id]
	if !ok || c.date != dateStr || now.Sub(c.at) >= s.gap() {
		return fetched{}, false
	}
	return c, true
}

// load fetches the child's data unless an attempt happened within the gap. It
// returns false when the load was throttled.
func (s *Service) load(ctx context.Context, m family.Member, dateStr string, day time.Time, now time.Time) (fetched, bool, error) {
	if s.throttled(m.ID, now) {
		return fetched{}, false, nil
	}
	in, name, err := s.Source.Load(ctx, m, day)
	if err != nil {
		return fetched{}, false, err
	}
	c := fetched{date: dateStr, in: in, name: name, at: now}
	s.cache[m.ID] = c
	return c, true, nil
}

// prefetch loads a child's data ahead of plan time (CLAUDE.md §5.1) so the plan
// is built from data fetched shortly before it is sent.
func (s *Service) prefetch(ctx context.Context, m family.Member, now time.Time) error {
	dateStr := planner.TargetDate(now, s.Loc)
	if prev, ok, err := s.Store.SentPlanFor(ctx, m.ID, dateStr); err != nil {
		return err
	} else if ok && prev.Hash == skipMarker {
		return nil
	}
	if _, ok := s.fresh(m.ID, dateStr, now); ok {
		return nil
	}
	day, err := time.ParseInLocation("2006-01-02", dateStr, s.Loc)
	if err != nil {
		return err
	}
	_, _, err = s.load(ctx, m, dateStr, day, now)
	return err
}

func (s *Service) processChild(ctx context.Context, m family.Member, parents []family.Member, now time.Time) (bool, error) {
	dateStr := planner.TargetDate(now, s.Loc)
	day, err := time.ParseInLocation("2006-01-02", dateStr, s.Loc)
	if err != nil {
		return false, err
	}
	prev, hasPrev, err := s.Store.SentPlanFor(ctx, m.ID, dateStr)
	if err != nil {
		return false, err
	}
	if hasPrev && prev.Hash == skipMarker {
		return false, nil
	}

	c, ok := s.fresh(m.ID, dateStr, now)
	if !ok {
		var loaded bool
		c, loaded, err = s.load(ctx, m, dateStr, day, now)
		if err != nil {
			return false, err
		}
		if !loaded {
			return false, nil
		}
	}
	firstName := c.name
	p := schedule.Build(c.in)
	text := ""
	if p.Skip == "" {
		if text, err = schedule.Render("", schedule.ChildView{FirstName: firstName, Plan: p}); err != nil {
			return false, err
		}
	}
	prevHash := ""
	if hasPrev {
		prevHash = prev.Hash
	}
	action := planner.Decide(p, text, prevHash)
	switch action {
	case planner.ActionNone:
		return false, nil
	case planner.ActionSkip:
		return false, s.Store.SavePlanSent(ctx, store.SentPlan{MemberID: m.ID, Date: dateStr, Hash: skipMarker})
	}

	hash := planner.Hash(text)
	if p.Skip != "" {
		// The day went empty after a plan was sent: tell the family and stop tracking.
		text = fmt.Sprintf("🔔 עדכון למערכת של מחר (%s): אין לימודים", dateStr)
		hash = skipMarker
	} else if action == planner.ActionUpdate {
		text = "🔔 עדכון למערכת של מחר:\n" + text
	}

	kind := "plan"
	if action == planner.ActionUpdate {
		kind = "plan_update"
	}
	eventID, err := s.recordEvent(ctx, m, dateStr, kind, hash, now)
	if err != nil {
		return false, err
	}
	if _, err := s.deliver(ctx, m, parents, eventID, text, p, now); err != nil {
		return false, err
	}
	if err := s.Store.SavePlanSent(ctx, store.SentPlan{MemberID: m.ID, Date: dateStr, Hash: hash, SentAt: now}); err != nil {
		return false, err
	}
	s.PlansSent.Inc()
	return true, nil
}

// ErrNoSchoolDay is returned by SendNow when the target day has no plan to send.
var ErrNoSchoolDay = errors.New("planjob: no plan for the target day")

// SendNow sends the child's plan for the next school day immediately, whether or
// not it was already sent, and returns how many deliveries were queued. It is
// recorded as its own event, so the scheduled plan and its late-change tracking
// are left unchanged. It reads Mashov directly; it is a user action, not a tick.
func (s *Service) SendNow(ctx context.Context, m family.Member, parents []family.Member) (int, error) {
	now := s.now()
	dateStr := planner.TargetDate(now, s.Loc)
	day, err := time.ParseInLocation("2006-01-02", dateStr, s.Loc)
	if err != nil {
		return 0, err
	}
	in, name, err := s.Source.Load(ctx, m, day)
	if err != nil {
		return 0, err
	}
	s.initMaps()
	s.cache[m.ID] = fetched{date: dateStr, in: in, name: name, at: now}
	p := schedule.Build(in)
	if p.Skip != "" {
		return 0, ErrNoSchoolDay
	}
	text, err := schedule.Render("", schedule.ChildView{FirstName: name, Plan: p})
	if err != nil {
		return 0, err
	}
	key := fmt.Sprintf("%s|%d|%s|manual|%d", Category, m.ID, dateStr, now.UnixNano())
	eventID, err := s.recordKey(ctx, key, "plan_manual", dateStr, now)
	if err != nil {
		return 0, err
	}
	return s.deliver(ctx, m, parents, eventID, text, p, now)
}

// deliver routes a rendered plan to the family and queues one delivery per target.
// Parents get the compact summary instead of the full text when the day has lessons.
func (s *Service) deliver(ctx context.Context, m family.Member, parents []family.Member, eventID int64, text string, p schedule.Plan, now time.Time) (int, error) {
	recipients, err := s.Store.Recipients(ctx, m.ID)
	if err != nil {
		return 0, err
	}
	muted := map[int64]bool{}
	for _, par := range parents {
		if ok, err := s.Store.IsMuted(ctx, par.ID, m.ID, Category); err != nil {
			return 0, err
		} else if ok {
			muted[par.ID] = true
		}
	}
	targets := routing.Route(routing.Input{Category: Category, Child: m, Overrides: recipients, Parents: parents, Muted: muted})
	for _, t := range targets {
		body := text
		if t.Audience == routing.AudienceParent && p.Skip == "" {
			body = fmt.Sprintf("📋 תוכנית המחר של %s: %d שיעורים", m.Name, len(p.Lessons))
		}
		if err := notify.Enqueue(ctx, s.Store, s.Loc, eventID, t, body, now); err != nil {
			return 0, err
		}
	}
	return len(targets), nil
}

// recordEvent stores the plan version once. A retry finds the same event.
func (s *Service) recordEvent(ctx context.Context, m family.Member, dateStr, kind, hash string, now time.Time) (int64, error) {
	return s.recordKey(ctx, fmt.Sprintf("%s|%d|%s|%s", Category, m.ID, dateStr, hash), kind, dateStr, now)
}

// recordKey stores an event under key once and returns its id.
func (s *Service) recordKey(ctx context.Context, key, kind, dateStr string, now time.Time) (int64, error) {
	data, _ := json.Marshal(map[string]string{"date": dateStr})
	inserted, err := s.Store.InsertEvent(ctx, store.Event{
		Category: Category, Kind: kind, OccurredAt: now, DetectedAt: now,
		Data: string(data), Source: "schedule", DedupeKey: key,
	})
	if err != nil {
		return 0, err
	}
	if inserted {
		s.Events.Inc(Category)
	}
	id, ok, err := s.Store.EventIDByDedupe(ctx, key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("event %q missing after insert", key)
	}
	return id, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// TomorrowText renders the child's plan for the next school day, as the child sees
// it. When there is no plan it returns the skip reason (for example "holiday")
// and empty text. It reads Mashov directly and is used for on-demand requests.
func (s *Service) TomorrowText(ctx context.Context, m family.Member) (text string, skip string, err error) {
	dateStr := planner.TargetDate(s.now(), s.Loc)
	day, err := time.ParseInLocation("2006-01-02", dateStr, s.Loc)
	if err != nil {
		return "", "", err
	}
	in, name, err := s.Source.Load(ctx, m, day)
	if err != nil {
		return "", "", err
	}
	p := schedule.Build(in)
	if p.Skip != "" {
		return "", p.Skip, nil
	}
	text, err = schedule.Render("", schedule.ChildView{FirstName: name, Plan: p})
	return text, "", err
}
