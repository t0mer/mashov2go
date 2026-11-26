package planner

import (
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/schedule"
)

func mustLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Jerusalem")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestTargetDateIsNextCivilDay(t *testing.T) {
	loc := mustLoc(t)
	// 23:30 local on Thursday 2026-11-26 still plans for Friday 2026-11-27.
	now := time.Date(2026, 11, 26, 23, 30, 0, 0, loc)
	if got := TargetDate(now, loc); got != "2026-11-27" {
		t.Errorf("got %s", got)
	}
	// Month boundary.
	now = time.Date(2026, 11, 30, 10, 0, 0, 0, loc)
	if got := TargetDate(now, loc); got != "2026-12-01" {
		t.Errorf("got %s", got)
	}
}

func TestPlanDueAndPrefetchWindows(t *testing.T) {
	loc := mustLoc(t)
	m := family.Member{PlanEnabled: true, PlanTime: "19:30"}
	at := func(h, min int) time.Time { return time.Date(2026, 11, 26, h, min, 0, 0, loc) }

	cases := []struct {
		now      time.Time
		due      bool
		prefetch bool
	}{
		{at(19, 14), false, false},
		{at(19, 15), false, true}, // 15 min before: prefetch starts
		{at(19, 29), false, true},
		{at(19, 30), true, false}, // plan time reached
		{at(23, 0), true, false},
	}
	for _, c := range cases {
		due, err := PlanDue(m, c.now, loc)
		if err != nil || due != c.due {
			t.Errorf("PlanDue(%s) = %v, %v; want %v", c.now.Format("15:04"), due, err, c.due)
		}
		pre, err := PrefetchDue(m, c.now, loc)
		if err != nil || pre != c.prefetch {
			t.Errorf("PrefetchDue(%s) = %v, %v; want %v", c.now.Format("15:04"), pre, err, c.prefetch)
		}
	}
}

func TestDisabledOrBadPlanTime(t *testing.T) {
	loc := mustLoc(t)
	now := time.Date(2026, 11, 26, 20, 0, 0, 0, loc)
	if due, _ := PlanDue(family.Member{PlanEnabled: false, PlanTime: "19:30"}, now, loc); due {
		t.Error("disabled plan is due")
	}
	if _, err := PlanDue(family.Member{PlanEnabled: true, PlanTime: "7pm"}, now, loc); err == nil {
		t.Error("bad plan time accepted")
	}
}

func TestDecide(t *testing.T) {
	full := schedule.Plan{}
	text := "lessons v1"
	cases := []struct {
		name string
		p    schedule.Plan
		prev string
		text string
		want Action
	}{
		{"first plan sends", full, "", text, ActionSend},
		{"unchanged plan is silent", full, Hash(text), text, ActionNone},
		{"late change updates", full, Hash(text), "lessons v2 lesson 3 cancelled", ActionUpdate},
		{"holiday with nothing sent is skipped", schedule.Plan{Skip: schedule.SkipHoliday}, "", "", ActionSkip},
		{"all cancelled after a send updates", schedule.Plan{Skip: schedule.SkipAllCancelled}, Hash(text), "", ActionUpdate},
	}
	for _, c := range cases {
		if got := Decide(c.p, c.text, c.prev); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}
