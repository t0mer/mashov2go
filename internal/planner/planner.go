// Package planner decides when a child's daily plan is due and whether a plan
// that was already sent needs a late-change update (CLAUDE.md §7).
package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/schedule"
)

// PrefetchLead is how long before plan time the timetable and changes are fetched.
const PrefetchLead = 15 * time.Minute

// Action is what to do with a plan at the moment it was built.
type Action string

const (
	ActionNone   Action = "none"   // nothing to send
	ActionSend   Action = "send"   // first plan for the date
	ActionUpdate Action = "update" // the plan changed after it was sent
	ActionSkip   Action = "skip"   // no school on the date; nothing was sent
)

// TargetDate returns the school day a plan built at now is for: the next civil
// day in loc, formatted YYYY-MM-DD.
func TargetDate(now time.Time, loc *time.Location) string {
	local := now.In(loc)
	next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc)
	return next.Format("2006-01-02")
}

// PlanDue reports whether the member's plan should be sent at now: plans are
// enabled and the local time has reached plan_time.
func PlanDue(m family.Member, now time.Time, loc *time.Location) (bool, error) {
	if !m.PlanEnabled {
		return false, nil
	}
	at, err := parseHM(m.PlanTime)
	if err != nil {
		return false, err
	}
	local := now.In(loc)
	planAt := time.Date(local.Year(), local.Month(), local.Day(), at.Hour(), at.Minute(), 0, 0, loc)
	return !local.Before(planAt), nil
}

// PrefetchDue reports whether the member's data should be fetched now: from
// PrefetchLead before plan time until plan time.
func PrefetchDue(m family.Member, now time.Time, loc *time.Location) (bool, error) {
	if !m.PlanEnabled {
		return false, nil
	}
	at, err := parseHM(m.PlanTime)
	if err != nil {
		return false, err
	}
	local := now.In(loc)
	planAt := time.Date(local.Year(), local.Month(), local.Day(), at.Hour(), at.Minute(), 0, 0, loc)
	return !local.Before(planAt.Add(-PrefetchLead)) && local.Before(planAt), nil
}

// Hash fingerprints the rendered plan text. A change in any lesson, note or
// homework line changes the hash.
func Hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Decide compares a freshly built plan with the one already sent for the same
// date. prevHash is "" when nothing was sent yet.
func Decide(p schedule.Plan, text string, prevHash string) Action {
	switch {
	case p.Skip != "" && prevHash == "":
		return ActionSkip
	case p.Skip != "":
		// A plan went out and the day is now empty (for example, all cancelled).
		return ActionUpdate
	case prevHash == "":
		return ActionSend
	case prevHash == Hash(text):
		return ActionNone
	default:
		return ActionUpdate
	}
}

func parseHM(s string) (time.Time, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return t, fmt.Errorf("plan time %q must be HH:MM", s)
	}
	return t, nil
}
