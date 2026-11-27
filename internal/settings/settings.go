// Package settings holds every functional setting (CLAUDE.md §12). Values live in
// the database; Service.Update validates and saves them, then notifies subscribers
// so the scheduler and other services apply the change without a restart.
package settings

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

const storeKey = "app"

// ErrInvalid marks a change that failed validation. Nothing was saved.
var ErrInvalid = errors.New("invalid settings")

// Settings is the full typed configuration.
type Settings struct {
	Timezone          string        `json:"timezone"`
	MashovMinGap      time.Duration `json:"mashovMinGap"`
	MashovProfile     string        `json:"mashovProfile"` // android | web
	SyncFastInterval  time.Duration `json:"syncFastInterval"`
	SyncFullInterval  time.Duration `json:"syncFullInterval"`
	SyncNightInterval time.Duration `json:"syncNightInterval"`
	ActiveHours       string        `json:"activeHours"`     // "06:30-22:30"
	PlanDefaultTime   string        `json:"planDefaultTime"` // "19:30"
	EventsRetention   time.Duration `json:"eventsRetention"`
	PreserveUnread    bool          `json:"preserveUnread"`
	ShabbatMode       bool          `json:"shabbatMode"`
}

// Defaults returns the seeded values (CLAUDE.md §12).
func Defaults() Settings {
	return Settings{
		Timezone:          "Asia/Jerusalem",
		MashovMinGap:      750 * time.Millisecond,
		MashovProfile:     "android",
		SyncFastInterval:  5 * time.Minute,
		SyncFullInterval:  60 * time.Minute,
		SyncNightInterval: 3 * time.Hour,
		ActiveHours:       "06:30-22:30",
		PlanDefaultTime:   "19:30",
		EventsRetention:   8760 * time.Hour,
		PreserveUnread:    true,
		ShabbatMode:       false,
	}
}

// Store is the persistence the service needs (implemented by *store.Store).
type Store interface {
	GetSetting(ctx context.Context, key string, into any) (bool, error)
	SetSetting(ctx context.Context, key string, v any) error
}

// Service owns the current settings and its subscribers.
type Service struct {
	store Store

	mu   sync.RWMutex
	cur  Settings
	subs []chan Settings
}

// Load reads settings from the store, filling unset keys with defaults.
func Load(ctx context.Context, st Store) (*Service, error) {
	cur := Defaults()
	if _, err := st.GetSetting(ctx, storeKey, &cur); err != nil {
		return nil, fmt.Errorf("loading settings: %w", err)
	}
	if err := cur.Validate(); err != nil {
		return nil, fmt.Errorf("stored settings are invalid: %w", err)
	}
	return &Service{store: st, cur: cur}, nil
}

// Get returns a copy of the current settings.
func (s *Service) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Update applies change to a copy, validates it, saves it, then notifies subscribers.
// Invalid settings are rejected and nothing changes.
func (s *Service) Update(ctx context.Context, change func(*Settings)) (Settings, error) {
	s.mu.Lock()
	next := s.cur
	change(&next)
	if err := next.Validate(); err != nil {
		s.mu.Unlock()
		return Settings{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := s.store.SetSetting(ctx, storeKey, next); err != nil {
		s.mu.Unlock()
		return Settings{}, fmt.Errorf("saving settings: %w", err)
	}
	s.cur = next
	subs := append([]chan Settings(nil), s.subs...)
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- next:
		default: // a slow subscriber keeps its latest pending value; never block a save
		}
	}
	return next, nil
}

// Subscribe returns a channel that receives every saved change. The buffer holds
// one value; a subscriber that falls behind sees only the newest change.
func (s *Service) Subscribe() <-chan Settings {
	ch := make(chan Settings, 1)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

var (
	hhmm        = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	activeRange = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d-([01]\d|2[0-3]):[0-5]\d$`)
)

// Validate checks every field. It is called on load and before every save.
func (s Settings) Validate() error {
	var errs []error
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		errs = append(errs, fmt.Errorf("timezone %q: %w", s.Timezone, err))
	}
	if s.MashovMinGap < 750*time.Millisecond {
		errs = append(errs, errors.New("mashovMinGap must be at least 750ms (CLAUDE.md §0.4)"))
	}
	if s.MashovProfile != "android" && s.MashovProfile != "web" {
		errs = append(errs, fmt.Errorf("mashovProfile must be android or web, got %q", s.MashovProfile))
	}
	for name, d := range map[string]time.Duration{
		"syncFastInterval": s.SyncFastInterval, "syncFullInterval": s.SyncFullInterval,
		"syncNightInterval": s.SyncNightInterval,
	} {
		if d < time.Minute {
			errs = append(errs, fmt.Errorf("%s must be at least 1m", name))
		}
	}
	if !activeRange.MatchString(s.ActiveHours) {
		errs = append(errs, fmt.Errorf("activeHours must look like 06:30-22:30, got %q", s.ActiveHours))
	}
	if !hhmm.MatchString(s.PlanDefaultTime) {
		errs = append(errs, fmt.Errorf("planDefaultTime must be HH:MM, got %q", s.PlanDefaultTime))
	}
	if s.EventsRetention < 24*time.Hour {
		errs = append(errs, errors.New("eventsRetention must be at least 24h"))
	}
	return errors.Join(errs...)
}
