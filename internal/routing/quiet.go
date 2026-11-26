package routing

import (
	"fmt"
	"time"
)

// HoldUntil returns when a message due at now may be sent, given a quiet window
// "HH:MM" to "HH:MM" in loc. Windows may cross midnight (21:00–07:00). When now is
// outside the window it returns now and false. Equal start and end means no quiet time.
func HoldUntil(now time.Time, start, end string, loc *time.Location) (time.Time, bool, error) {
	s, err := parseHM(start)
	if err != nil {
		return now, false, err
	}
	e, err := parseHM(end)
	if err != nil {
		return now, false, err
	}
	if s == e {
		return now, false, nil
	}
	local := now.In(loc)
	mins := local.Hour()*60 + local.Minute()

	var inWindow bool
	if s < e { // same-day window, e.g. 13:00–14:00
		inWindow = mins >= s && mins < e
	} else { // crosses midnight, e.g. 21:00–07:00
		inWindow = mins >= s || mins < e
	}
	if !inWindow {
		return now, false, nil
	}
	// Release at the next occurrence of the end time, strictly after now.
	release := time.Date(local.Year(), local.Month(), local.Day(), e/60, e%60, 0, 0, loc)
	if !release.After(local) {
		release = release.AddDate(0, 0, 1)
	}
	return release, true, nil
}

func parseHM(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("quiet time %q must be HH:MM", s)
	}
	return h*60 + m, nil
}
