// Package schedule builds a child's daily plan from Mashov timetable data.
// All dates are civil (calendar) days; callers pass the Asia/Jerusalem date
// components and this package never shifts them by timezone or DST.
package schedule

import "time"

// civil returns t's calendar date at UTC midnight.
func civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// parseDay reads the date part of a Mashov timestamp ("2026-10-16T00:00:00").
func parseDay(s string) (time.Time, bool) {
	if len(s) < 10 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s[:10])
	return t, err == nil
}

// weekStart is the Sunday on or before d.
func weekStart(d time.Time) time.Time {
	return d.AddDate(0, 0, -int(d.Weekday()))
}
