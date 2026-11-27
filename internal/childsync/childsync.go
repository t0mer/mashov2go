// Package childsync syncs a child's school domains (grades, behaviour) and tells
// the family about new and changed records (CLAUDE.md §5, §6.2). Each domain has
// its own routing category and default recipients. The first sync of a domain for
// a child is a silent baseline, so existing records are not announced. Later syncs
// queue one message per new or updated record.
package childsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/notify"
	"github.com/t0mer/mashov2go/internal/routing"
	"github.com/t0mer/mashov2go/internal/schedule"
	"github.com/t0mer/mashov2go/internal/store"
	"github.com/t0mer/mashov2go/internal/syncer"
	"github.com/t0mer/mashov2go/internal/templates"
)

// DefaultGap is how often a child's domains are fetched when no setting says otherwise.
const DefaultGap = time.Hour

// Domain describes one synced school domain.
type Domain struct {
	// Name is the syncer domain ("grades", "behave"): it selects the item key rule
	// and the event category.
	Name string
	// Category is the routing category (family.CatGrades, family.CatBehave).
	Category string
	// Fetch returns the child's records for a window, one value per record.
	Fetch func(ctx context.Context, child family.Member, from, to time.Time) ([]any, error)
	// Data turns a stored record into template data. kind is the event kind
	// ("<domain>.new" or "<domain>.updated") and payload the stored record. The
	// message text is rendered per recipient from the domain's template.
	Data func(child, kind, payload string) (map[string]any, error)
}

// Service runs the sync for every linked child and every domain.
type Service struct {
	Store   *store.Store
	Syncer  *syncer.Syncer
	Loc     *time.Location
	Now     func() time.Time
	Domains []Domain
	// Gap returns the time between two fetches of one child's domain; nil uses DefaultGap.
	Gap func() time.Duration
	// Templates renders the message text (nil uses the built-in defaults only).
	Templates templates.Renderer

	lastRun map[string]time.Time
}

// RunDue fetches each active linked child's domains whose last fetch is older than
// the gap. It returns how many messages it queued. One failure does not stop the
// others; the errors are joined in the result.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	now := s.now()
	if s.lastRun == nil {
		s.lastRun = map[string]time.Time{}
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
		for _, d := range s.Domains {
			key := fmt.Sprintf("%d|%s", m.ID, d.Name)
			if last, ok := s.lastRun[key]; ok && now.Sub(last) < s.gap() {
				continue
			}
			s.lastRun[key] = now // set before fetching, so a failing domain also waits
			n, err := s.syncDomain(ctx, d, m, parents, now)
			if err != nil {
				errs = append(errs, fmt.Errorf("child %d %s: %w", m.ID, d.Name, err))
				continue
			}
			queued += n
		}
	}
	return queued, errors.Join(errs...)
}

// Invalidate makes the next RunDue fetch the child's domain even inside the gap.
// The fast notification loop uses it when a notification points at that domain.
func (s *Service) Invalidate(memberID int64, domain string) {
	if s.lastRun == nil {
		return
	}
	delete(s.lastRun, fmt.Sprintf("%d|%s", memberID, domain))
}

// NotificationDomains returns the sync domains a Mashov notification type refreshes
// (CLAUDE.md §5.2). Types with no domain here return nil.
func NotificationDomains(eventType int) []string {
	switch eventType {
	case 111: // Grade
		return []string{"grades"}
	case 101: // Behave
		return []string{"behave"}
	case 102: // Homework
		return []string{"homework"}
	}
	return nil
}

func (s *Service) syncDomain(ctx context.Context, d Domain, m family.Member, parents []family.Member, now time.Time) (int, error) {
	accountID, guid, linked, err := s.Store.MemberLink(ctx, m.ID)
	if err != nil {
		return 0, err
	}
	if !linked {
		return 0, nil
	}
	items, err := d.Fetch(ctx, m, schoolYearStart(now), now.AddDate(0, 0, 1))
	if err != nil {
		return 0, err
	}
	raw := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		b, err := json.Marshal(it)
		if err != nil {
			return 0, err
		}
		raw = append(raw, b)
	}
	res, err := s.Syncer.Apply(ctx, accountID, guid, d.Name, raw)
	if err != nil {
		return 0, err
	}
	if res.Baseline || len(res.Events) == 0 {
		return 0, nil
	}
	recipients, err := s.Store.Recipients(ctx, m.ID)
	if err != nil {
		return 0, err
	}
	muted := map[int64]bool{}
	for _, p := range parents {
		if ok, err := s.Store.IsMuted(ctx, p.ID, m.ID, d.Category); err != nil {
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
		data, err := d.Data(m.Name, ev.Kind, ev.Data)
		if err != nil {
			return queued, err
		}
		targets := routing.Route(routing.Input{Category: d.Category, Child: m, Overrides: recipients, Parents: parents, Muted: muted})
		for _, t := range targets {
			body, err := s.Templates.Render(ctx, d.Name, t.Member.Language, t.Audience, data)
			if err != nil {
				return queued, err
			}
			if err := notify.Enqueue(ctx, s.Store, s.Loc, eventID, t, body, now); err != nil {
				return queued, err
			}
			queued++
		}
	}
	return queued, nil
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
