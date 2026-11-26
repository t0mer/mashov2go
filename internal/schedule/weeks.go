package schedule

import "time"

// SchoolWeek returns the school-year week number of d (CLAUDE.md §2, §7).
// The school year starts at the Sunday-starting week that contains September 1
// of schoolYearEnd-1, and that week is week 1.
func SchoolWeek(d time.Time, schoolYearEnd int) int {
	d = civil(d)
	sept1 := time.Date(schoolYearEnd-1, time.September, 1, 0, 0, 0, 0, time.UTC)
	days := int(weekStart(d).Sub(weekStart(sept1)).Hours() / 24)
	return days/7 + 1
}

// SchoolYearEnd returns the `year` value Mashov uses for d: the calendar year the
// school year ends (2026-27 -> 2027). Dates from September onward belong to the
// following year's school year.
func SchoolYearEnd(d time.Time) int {
	if d.Month() >= time.September {
		return d.Year() + 1
	}
	return d.Year()
}

// LessonRunsInWeek reports whether a timetable `weeks` bitmask schedules a lesson
// in school week n. Per CLAUDE.md §2 (isTimeTableScheduledWeek): weeks of nil/0 or
// -1 mean every week; otherwise bit n must be set.
func LessonRunsInWeek(weeks int, n int) bool {
	if weeks == 0 || weeks == -1 {
		return true
	}
	if n < 0 || n >= 64 {
		return false
	}
	return (uint64(weeks)>>uint(n))&1 == 1
}
