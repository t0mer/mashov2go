// Package mashovsource loads a child's school day from Mashov for the daily
// plan (CLAUDE.md §3, §4, §7). It owns one Mashov session per account, logs in
// with the stored devicePass when there is one, and falls back to the password
// at most once per cooldown. It is the only code path from the plan job to
// Mashov. It never logs response bodies or names.
package mashovsource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/schedule"
	"github.com/t0mer/mashov2go/internal/store"
)

// PasswordCooldown is the minimum time between two password logins for one
// account (CLAUDE.md §3.3: a password login is never retried automatically).
const PasswordCooldown = 30 * time.Minute

// ErrNotLinked is returned when a child member has no Mashov student link.
var ErrNotLinked = errors.New("mashovsource: child is not linked to a Mashov student")

// ErrPasswordCooldown is returned when a password login was tried too recently.
var ErrPasswordCooldown = errors.New("mashovsource: password login cooling down")

// Source implements planjob.Source.
type Source struct {
	Store *store.Store
	// BaseURL overrides mashov.DefaultBaseURL (tests point it at httptest).
	BaseURL string
	// MinGap overrides the client's request spacing when non-zero.
	MinGap    time.Duration
	Transport http.RoundTripper
	Now       func() time.Time

	mu           sync.Mutex
	sessions     map[int64]*mashov.Client
	lastPassword map[int64]time.Time
}

// Load fetches everything the plan for day needs. The first name returned is the
// child member's first word, which matches how the plan greets the child.
func (s *Source) Load(ctx context.Context, child family.Member, day time.Time) (schedule.Input, string, error) {
	accountID, guid, ok, err := s.Store.MemberLink(ctx, child.ID)
	if err != nil {
		return schedule.Input{}, "", err
	}
	if !ok {
		return schedule.Input{}, "", ErrNotLinked
	}
	c, err := s.session(ctx, accountID)
	if err != nil {
		return schedule.Input{}, "", err
	}
	in, err := s.fetch(ctx, c, guid, day)
	if errors.Is(err, mashov.ErrSessionExpired) {
		// Drop the session so the next tick logs in again.
		s.drop(accountID)
	}
	if err != nil {
		return schedule.Input{}, "", err
	}
	in.IncludeHomeworkGivenToday = true
	return in, firstName(child.Name), nil
}

func (s *Source) fetch(ctx context.Context, c *mashov.Client, guid string, day time.Time) (schedule.Input, error) {
	var in schedule.Input
	in.Date = day
	var err error
	if in.Lessons, err = c.Timetable(ctx, guid); err != nil {
		return in, fmt.Errorf("timetable: %w", err)
	}
	// A window around the day; the builder matches the exact date.
	if in.Changes, err = c.TimetableChangesFor(ctx, guid, day.AddDate(0, 0, -1), day.AddDate(0, 0, 2)); err != nil {
		return in, fmt.Errorf("timetable changes: %w", err)
	}
	if in.Bells, err = c.Bells(ctx); err != nil {
		return in, fmt.Errorf("bells: %w", err)
	}
	if in.Holidays, err = c.Holidays(ctx); err != nil {
		return in, fmt.Errorf("holidays: %w", err)
	}
	if in.Homework, err = c.Homework(ctx, guid); err != nil {
		return in, fmt.Errorf("homework: %w", err)
	}
	return in, nil
}

// session returns the account's client, logging in on first use.
func (s *Source) session(ctx context.Context, accountID int64) (*mashov.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[int64]*mashov.Client{}
		s.lastPassword = map[int64]time.Time{}
	}
	if c, ok := s.sessions[accountID]; ok {
		return c, nil
	}
	c, err := mashov.NewClient(mashov.Options{
		BaseURL: s.BaseURL, MinGap: s.MinGap, Transport: s.Transport, Profile: mashov.AndroidProfile,
	})
	if err != nil {
		return nil, err
	}
	if err := s.login(ctx, c, accountID); err != nil {
		return nil, err
	}
	c.SetReauth(func(ctx context.Context) error {
		// Reauth runs on the request path, outside session(); take the lock so
		// the password cooldown map is never touched concurrently.
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.login(ctx, c, accountID)
	})
	s.sessions[accountID] = c
	return c, nil
}

func (s *Source) drop(accountID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, accountID)
}

// login signs c in for the account. It prefers the stored devicePass, clears it
// when Mashov rejects it, and then tries the password once per cooldown. Login
// failures set the account's auth state so the UI can show what to do.
func (s *Source) login(ctx context.Context, c *mashov.Client, accountID int64) error {
	acc, err := s.Store.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	p := mashov.AndroidProfile
	fields := mashov.DeviceFields{
		AppName: p.AppName, APIVersion: p.APIVersion, AppVersion: p.AppVersion, AppBuild: p.AppBuild,
		DeviceUUID: acc.DeviceUUID, DevicePlatform: p.DevicePlatform,
		DeviceManufacturer: p.DeviceManufacturer, DeviceModel: p.DeviceModel, DeviceVersion: p.DeviceVersion,
	}

	var res *mashov.LoginResponse
	if acc.DevicePass != "" {
		res, err = c.LoginDevice(ctx, fields, acc.DevicePass)
		if err != nil {
			var le *mashov.LoginError
			if !errors.As(err, &le) {
				return fmt.Errorf("device login: %w", err)
			}
			// Revoked or rejected: forget it and fall back to the password.
			if err := s.Store.UpdateDevicePass(ctx, accountID, ""); err != nil {
				return err
			}
			res = nil
		}
	}
	if res == nil {
		now := s.now()
		if last, ok := s.lastPassword[accountID]; ok && now.Sub(last) < PasswordCooldown {
			return ErrPasswordCooldown
		}
		s.lastPassword[accountID] = now
		res, err = c.Login(ctx, mashov.LoginRequest{
			Semel: acc.Semel, Year: acc.Year, Username: acc.Username, Password: acc.Password,
			IsPrivateDevice: true, DeviceFields: fields,
		}, "")
		if err != nil {
			var le *mashov.LoginError
			if errors.As(err, &le) {
				_ = s.Store.SetAuthState(ctx, accountID, string(le.State))
			}
			return fmt.Errorf("password login: %w", err)
		}
	}
	if res.DevicePass != "" {
		if err := s.Store.UpdateDevicePass(ctx, accountID, res.DevicePass); err != nil {
			return err
		}
	}
	if acc.AuthState != string(mashov.StateOK) {
		if err := s.Store.SetAuthState(ctx, accountID, string(mashov.StateOK)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// firstName returns the first word of a display name ("" when empty).
func firstName(name string) string {
	f := strings.Fields(name)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}
