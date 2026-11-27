// Package fastsync polls each Mashov account's notification feed and, when a
// notification names a domain (grades, behaviour), asks the child sync to fetch
// that child now instead of at the next full sync (CLAUDE.md §5.1, §5.2). It runs
// only inside the active hours.
package fastsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/t0mer/mashov2go/internal/childsync"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/store"
	"github.com/t0mer/mashov2go/internal/syncer"
)

// Source reads an account's notification feed (implemented by mashovsource.Source).
type Source interface {
	Notifications(ctx context.Context, accountID int64) ([]mashov.Notification, error)
}

// Invalidator marks a child's domain as due (implemented by childsync.Service).
type Invalidator interface {
	Invalidate(memberID int64, domain string)
}

// Service runs the fast loop.
type Service struct {
	Store  *store.Store
	Source Source
	Child  Invalidator
	Loc    *time.Location
	Now    func() time.Time
	// Interval returns the time between two polls of one account (SyncFastInterval).
	Interval func() time.Duration
	// ActiveHours returns the window, e.g. "06:30-22:30" (ActiveHours setting).
	ActiveHours func() string

	lastPoll map[int64]time.Time
}

// seenKey stores the newest notification id processed for an account.
func seenKey(accountID int64) string {
	return "notifications.seen." + strconv.FormatInt(accountID, 10)
}

// RunDue polls the accounts that are due and invalidates the child domains that
// new notifications point at. It returns how many domains it invalidated. The
// first poll of an account only records the newest id, so an old feed does not
// trigger a burst of fetches.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	now := s.now()
	w, err := syncer.ParseWindow(s.activeHours())
	if err != nil {
		return 0, err
	}
	if !w.Contains(now.In(s.Loc)) {
		return 0, nil
	}
	if s.lastPoll == nil {
		s.lastPoll = map[int64]time.Time{}
	}
	accounts, err := s.Store.ListAccounts(ctx)
	if err != nil {
		return 0, err
	}
	invalidated := 0
	var errs []error
	for _, acc := range accounts {
		if last, ok := s.lastPoll[acc.ID]; ok && now.Sub(last) < s.interval() {
			continue
		}
		s.lastPoll[acc.ID] = now // set before polling, so a failing account also waits
		n, err := s.pollAccount(ctx, acc.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("account %d: %w", acc.ID, err))
			continue
		}
		invalidated += n
	}
	return invalidated, errors.Join(errs...)
}

func (s *Service) pollAccount(ctx context.Context, accountID int64) (int, error) {
	list, err := s.Source.Notifications(ctx, accountID)
	if err != nil {
		return 0, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID.Int() < list[j].ID.Int() })
	key := seenKey(accountID)
	var seen int64
	found, err := s.Store.GetSetting(ctx, key, &seen)
	if err != nil {
		return 0, err
	}
	newest := seen
	if !found {
		// First poll: record the newest id and fetch nothing.
		for _, n := range list {
			newest = max(newest, int64(n.ID.Int()))
		}
		return 0, s.Store.SetSetting(ctx, key, newest)
	}
	invalidated := 0
	for _, n := range list {
		id := int64(n.ID.Int())
		if id <= seen {
			continue
		}
		newest = max(newest, id)
		domains := childsync.NotificationDomains(n.EventType.Int())
		if len(domains) == 0 {
			continue
		}
		child, err := s.Store.ChildMemberFor(ctx, accountID, n.EventUserID)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			continue // the item is about someone who is not a linked child
		}
		if err != nil {
			return invalidated, err
		}
		for _, d := range domains {
			s.Child.Invalidate(child.ID, d)
			invalidated++
		}
	}
	if newest != seen {
		if err := s.Store.SetSetting(ctx, key, newest); err != nil {
			return invalidated, err
		}
	}
	return invalidated, nil
}

func (s *Service) interval() time.Duration {
	if s.Interval != nil {
		return s.Interval()
	}
	return 5 * time.Minute
}

func (s *Service) activeHours() string {
	if s.ActiveHours != nil {
		return s.ActiveHours()
	}
	return "06:30-22:30"
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
