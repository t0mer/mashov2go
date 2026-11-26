package schedule

import (
	"testing"
	"time"
)

func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

func TestSchoolYearEnd(t *testing.T) {
	cases := []struct {
		date time.Time
		want int
	}{
		{d(2026, time.August, 31), 2026},
		{d(2026, time.September, 1), 2027},
		{d(2027, time.January, 15), 2027},
	}
	for _, c := range cases {
		if got := SchoolYearEnd(c.date); got != c.want {
			t.Errorf("SchoolYearEnd(%s) = %d, want %d", c.date.Format("2006-01-02"), got, c.want)
		}
	}
}

// Week 1 is the Sunday-starting week containing Sept 1. Table covers each weekday
// of Sept 1 so the boundary moves correctly.
func TestSchoolWeekAroundSeptemberFirst(t *testing.T) {
	cases := []struct {
		name string
		date time.Time
		want int
	}{
		// 2026-09-01 is a Tuesday: its Sunday-start week is Aug 30 - Sep 5.
		{"sept1 tuesday", d(2026, time.September, 1), 1},
		{"sunday before sept1 is last week of prior year", d(2026, time.August, 30), 53},
		{"saturday of week 1", d(2026, time.September, 5), 1},
		{"sunday starting week 2", d(2026, time.September, 6), 2},
		// 2025-09-01 is a Monday: its week is Aug 31 - Sep 6.
		{"sept1 monday", d(2025, time.September, 1), 1},
		{"sunday Aug31 2025 is last week of prior year", d(2025, time.August, 31), 53},
		// 2023-09-01 is a Friday: week is Aug 27 - Sep 2.
		{"sept1 friday", d(2023, time.September, 1), 1},
		{"sunday Sep3 2023 is week 2", d(2023, time.September, 3), 2},
		// Late in the year: Jun 1 2027 is in school year ending 2027.
		{"june 2027", d(2027, time.June, 1), 40},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			year := SchoolYearEnd(c.date)
			if got := SchoolWeek(c.date, year); got != c.want {
				t.Fatalf("SchoolWeek(%s) = %d, want %d", c.date.Format("2006-01-02"), got, c.want)
			}
		})
	}
}

func TestLessonRunsInWeek(t *testing.T) {
	cases := []struct {
		weeks, n int
		want     bool
	}{
		{0, 3, true},  // null/0: every week
		{-1, 3, true}, // -1: every week
		{0b0101, 0, true},
		{0b0101, 1, false},
		{0b0101, 2, true},
		{0b10, 1, true},
		{0b10, 2, false},
		{1 << 40, 40, true},
		{0b1, 70, false}, // out of range: not scheduled
	}
	for _, c := range cases {
		if got := LessonRunsInWeek(c.weeks, c.n); got != c.want {
			t.Errorf("LessonRunsInWeek(%b, %d) = %v, want %v", c.weeks, c.n, got, c.want)
		}
	}
}
