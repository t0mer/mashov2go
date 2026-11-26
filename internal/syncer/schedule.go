package syncer

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// Jitter returns base shifted by a uniform random factor in [1-frac, 1+frac]
// (CLAUDE.md §5.1: ±15 %).
func Jitter(base time.Duration, frac float64, r func() float64) time.Duration {
	if r == nil {
		r = rand.Float64
	}
	f := 1 - frac + r()*2*frac
	return time.Duration(float64(base) * f)
}

// ActiveWindow is a daily window such as 06:30-22:30 in a location.
type ActiveWindow struct {
	Start, End time.Duration // offsets from local midnight
}

// ParseWindow parses "HH:MM-HH:MM".
func ParseWindow(s string) (ActiveWindow, error) {
	var sh, sm, eh, em int
	if _, err := fmt.Sscanf(s, "%d:%d-%d:%d", &sh, &sm, &eh, &em); err != nil {
		return ActiveWindow{}, fmt.Errorf("active window %q: %w", s, err)
	}
	return ActiveWindow{
		Start: time.Duration(sh)*time.Hour + time.Duration(sm)*time.Minute,
		End:   time.Duration(eh)*time.Hour + time.Duration(em)*time.Minute,
	}, nil
}

// Contains reports whether t falls inside the window in t's location.
func (w ActiveWindow) Contains(t time.Time) bool {
	since := time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute
	return since >= w.Start && since < w.End
}
