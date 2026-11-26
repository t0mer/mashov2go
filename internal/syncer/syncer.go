// Package syncer applies one fetch of a (child, domain) to storage and emits
// events (CLAUDE.md §5). Network access happens in the caller; this package is
// given the fetched records.
package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/t0mer/mashov2go/internal/differ"
	"github.com/t0mer/mashov2go/internal/store"
)

// Syncer writes fetch results and events.
type Syncer struct {
	Store  *store.Store
	Broker *Broker // optional; receives each new event after it is stored
	Now    func() time.Time
}

// Result summarises one applied fetch.
type Result struct {
	Baseline bool
	New      int
	Updated  int
	Removed  int
	Events   []store.Event
}

// Categories map Mashov domains to the event categories of CLAUDE.md §6.2.
var categoryOf = map[string]string{
	"homework":          "homework",
	"timetable-changes": "timetable_change",
	"grades":            "grades",
	"behave":            "behave",
	"maakav":            "maakav",
	"messageboard":      "school_notice",
	"justifications":    "school_notice",
}

// Apply stores one fetch of records for (account, child, domain). The first fetch
// of a domain is a silent baseline. Later fetches emit one event per new or
// updated record; removed records are soft-deleted without an event.
func (s *Syncer) Apply(ctx context.Context, accountID int64, childGUID, domain string, raw []json.RawMessage) (Result, error) {
	now := s.now()
	cat, ok := categoryOf[domain]
	if !ok {
		return Result{}, fmt.Errorf("domain %q has no event category", domain)
	}

	cur, err := differ.Normalize(domain, raw)
	if err != nil {
		return Result{}, err
	}
	baseline, err := s.Store.IsBaselined(ctx, accountID, childGUID, domain)
	if err != nil {
		return Result{}, err
	}
	prev, err := s.Store.ListItems(ctx, accountID, childGUID, domain)
	if err != nil {
		return Result{}, err
	}
	res := differ.Classify(prev, cur, !baseline)

	// Events are written before item state. If the process stops in between, the
	// next sync still sees the change and the dedupe key stops a second announcement.
	out := Result{Baseline: !baseline, Removed: len(res.Removed)}
	for _, ch := range res.Changes {
		kind := fmt.Sprintf("%s.%s", domainKind(domain), ch.Kind)
		ev := store.Event{
			Category:   cat,
			Kind:       kind,
			ChildGUID:  childGUID,
			OccurredAt: now,
			DetectedAt: now,
			Data:       ch.Current.Payload,
			Source:     "diff",
			DedupeKey:  childGUID + "|" + domain + "|" + ch.Current.Key + "|" + ch.Current.Hash,
		}
		inserted, err := s.Store.InsertEvent(ctx, ev)
		if err != nil {
			return out, err
		}
		if !inserted {
			continue // already reported by an earlier sync
		}
		if ch.Kind == "new" {
			out.New++
		} else {
			out.Updated++
		}
		out.Events = append(out.Events, ev)
		if s.Broker != nil {
			s.Broker.Publish(ev)
		}
	}
	for _, c := range cur {
		if err := s.Store.PutItem(ctx, store.Item{
			AccountID: accountID, ChildGUID: childGUID, Domain: domain,
			Key: c.Key, ContentHash: c.Hash, Payload: c.Payload,
		}, now); err != nil {
			return Result{}, err
		}
	}
	for _, k := range res.Removed {
		if err := s.Store.SoftDeleteItem(ctx, accountID, childGUID, domain, k, now); err != nil {
			return Result{}, err
		}
	}
	if err := s.Store.MarkSynced(ctx, accountID, childGUID, domain, now); err != nil {
		return Result{}, err
	}

	return out, nil
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// domainKind is the event kind prefix, e.g. "homework" in "homework.new".
func domainKind(domain string) string {
	return categoryOf[domain]
}
