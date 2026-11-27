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

	lastLoad map[int64]time.Time
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

// throttled reports whether the child was loaded too recently, and records the
// load time when it was not. The time is recorded before loading, so a failing
// child also waits out the gap instead of being retried on every tick.
func (s *Service) throttled(id int64, now time.Time) bool {
	if s.lastLoad == nil {
		s.lastLoad = map[int64]time.Time{}
	}
	gap := s.LoadGap
	if gap == 0 {
		gap = DefaultLoadGap
	}
	if last, ok := s.lastLoad[id]; ok && now.Sub(last) < gap {
		return true
	}
	s.lastLoad[id] = now
	return false
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

	if s.throttled(m.ID, now) {
		return false, nil
	}
	in, firstName, err := s.Source.Load(ctx, m, day)
	if err != nil {
		return false, err
	}
	p := schedule.Build(in)
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

	recipients, err := s.Store.Recipients(ctx, m.ID)
	if err != nil {
		return false, err
	}
	muted := map[int64]bool{}
	for _, p := range parents {
		if ok, err := s.Store.IsMuted(ctx, p.ID, m.ID, Category); err != nil {
			return false, err
		} else if ok {
			muted[p.ID] = true
		}
	}
	targets := routing.Route(routing.Input{Category: Category, Child: m, Overrides: recipients, Parents: parents, Muted: muted})
	for _, t := range targets {
		body := text
		if t.Audience == routing.AudienceParent && p.Skip == "" {
			body = fmt.Sprintf("📋 תוכנית המחר של %s: %d שיעורים", m.Name, len(p.Lessons))
		}
		if err := s.enqueue(ctx, eventID, t, body, now); err != nil {
			return false, err
		}
	}
	if err := s.Store.SavePlanSent(ctx, store.SentPlan{MemberID: m.ID, Date: dateStr, Hash: hash, SentAt: now}); err != nil {
		return false, err
	}
	return true, nil
}

// recordEvent stores the plan version once. A retry finds the same event.
func (s *Service) recordEvent(ctx context.Context, m family.Member, dateStr, kind, hash string, now time.Time) (int64, error) {
	key := fmt.Sprintf("%s|%d|%s|%s", Category, m.ID, dateStr, hash)
	data, _ := json.Marshal(map[string]string{"date": dateStr})
	if _, err := s.Store.InsertEvent(ctx, store.Event{
		Category: Category, Kind: kind, OccurredAt: now, DetectedAt: now,
		Data: string(data), Source: "schedule", DedupeKey: key,
	}); err != nil {
		return 0, err
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

// enqueue queues one target (a duplicate for the same event and member is a no-op). Quiet hours move the send to the end of the hold.
// A target with no usable channel is recorded as skipped_no_channel.
func (s *Service) enqueue(ctx context.Context, eventID int64, t routing.Target, body string, now time.Time) error {
	when := now
	if t.Member.QuietStart != "" && t.Member.QuietEnd != "" {
		until, held, err := routing.HoldUntil(now, t.Member.QuietStart, t.Member.QuietEnd, s.Loc)
		if err != nil {
			return err
		}
		if held {
			when = until
		}
	}
	d := store.Delivery{EventID: eventID, MemberID: t.Member.ID, Channel: t.Channel, Body: body, NextAt: when}
	if t.SkipReason != "" {
		d.Status, d.LastError = "skipped_no_channel", t.SkipReason
	}
	_, err := s.Store.EnqueueDelivery(ctx, d)
	return err
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
