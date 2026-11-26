package routing

import (
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
)

func kid() family.Member {
	return family.Member{ID: 1, Role: family.RoleChild, Name: "Noa", Channel: family.ChannelWhatsApp,
		WhatsAppPhone: "972502222222", Active: true}
}

func parentM(id int64, phone string) family.Member {
	return family.Member{ID: id, Role: family.RoleParent, Name: "P", Channel: family.ChannelWhatsApp,
		WhatsAppPhone: phone, Active: true}
}

func audiences(ts []Target) map[int64]string {
	m := map[int64]string{}
	for _, t := range ts {
		m[t.Member.ID] = t.Audience
	}
	return m
}

func TestDefaultsRouteAsCategorised(t *testing.T) {
	parents := []family.Member{parentM(10, "972501111111"), parentM(11, "972503333333")}
	// homework defaults to the child only.
	if ts := Route(Input{Category: family.CatHomework, Child: kid(), Parents: parents}); len(ts) != 1 || ts[0].Audience != AudienceChild {
		t.Fatalf("homework targets = %+v", ts)
	}
	// grades default to parents only.
	got := audiences(Route(Input{Category: family.CatGrades, Child: kid(), Parents: parents}))
	if len(got) != 2 || got[10] != AudienceParent || got[11] != AudienceParent {
		t.Fatalf("grades targets = %v", got)
	}
	// timetable changes default to both.
	if got := audiences(Route(Input{Category: family.CatTimetableChange, Child: kid(), Parents: parents})); len(got) != 3 {
		t.Fatalf("timetable_change targets = %v", got)
	}
}

func TestOverridesAndOff(t *testing.T) {
	parents := []family.Member{parentM(10, "972501111111")}
	off := Route(Input{Category: family.CatGrades, Child: kid(), Parents: parents,
		Overrides: map[string]family.Recipient{family.CatGrades: family.ToOff}})
	if len(off) != 0 {
		t.Fatalf("off must route nothing: %+v", off)
	}
	child := Route(Input{Category: family.CatGrades, Child: kid(), Parents: parents,
		Overrides: map[string]family.Recipient{family.CatGrades: family.ToChild}})
	if len(child) != 1 || child[0].Member.ID != 1 {
		t.Fatalf("override to child: %+v", child)
	}
}

func TestMutedAndInactiveParentsAreSkipped(t *testing.T) {
	quiet := parentM(10, "972501111111")
	gone := parentM(11, "972503333333")
	gone.Active = false
	ts := Route(Input{Category: family.CatBehave, Child: kid(),
		Parents: []family.Member{quiet, gone}, Muted: map[int64]bool{10: true}})
	if len(ts) != 0 {
		t.Fatalf("muted/inactive parents received: %+v", ts)
	}
}

func TestInactiveChildHasNoChannel(t *testing.T) {
	k := kid()
	k.Active = false
	ts := Route(Input{Category: family.CatHomework, Child: k})
	if len(ts) != 1 || ts[0].SkipReason != "inactive" || ts[0].Channel != "" {
		t.Fatalf("inactive child must carry no channel: %+v", ts)
	}
}

func TestFallbackAndNoChannel(t *testing.T) {
	p := parentM(10, "")
	p.Channel = family.ChannelWhatsApp
	p.FallbackChannel = family.ChannelTelegram
	p.TelegramChatID = 555
	ts := Route(Input{Category: family.CatGrades, Child: kid(), Parents: []family.Member{p}})
	if len(ts) != 1 || ts[0].Channel != family.ChannelTelegram || ts[0].SkipReason != "" {
		t.Fatalf("fallback not used: %+v", ts)
	}

	bare := parentM(11, "")
	ts = Route(Input{Category: family.CatGrades, Child: kid(), Parents: []family.Member{bare}})
	if len(ts) != 1 || ts[0].SkipReason != "no_channel" || ts[0].Channel != "" {
		t.Fatalf("expected no_channel skip: %+v", ts)
	}
}

func TestUnknownCategoryDefaultsOff(t *testing.T) {
	if r := Recipient("nope", nil); r != family.ToOff {
		t.Fatalf("unknown category recipient = %s", r)
	}
}

func TestHoldUntil(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	at := func(h, m int) time.Time { return time.Date(2026, 11, 26, h, m, 0, 0, loc) }

	// 23:00 is inside 21:00–07:00 -> released at 07:00 next day.
	rel, held, err := HoldUntil(at(23, 0), "21:00", "07:00", loc)
	if err != nil || !held || !rel.Equal(time.Date(2026, 11, 27, 7, 0, 0, 0, loc)) {
		t.Fatalf("23:00 held=%v release=%v err=%v", held, rel, err)
	}
	// 03:00 is inside the window -> released at 07:00 the same day.
	rel, held, _ = HoldUntil(at(3, 0), "21:00", "07:00", loc)
	if !held || !rel.Equal(time.Date(2026, 11, 26, 7, 0, 0, 0, loc)) {
		t.Fatalf("03:00 release = %v held=%v", rel, held)
	}
	// 12:00 is outside.
	if _, held, _ := HoldUntil(at(12, 0), "21:00", "07:00", loc); held {
		t.Fatal("noon must not be held")
	}
	// Same-day window 13:00–14:00.
	if _, held, _ := HoldUntil(at(13, 30), "13:00", "14:00", loc); !held {
		t.Fatal("13:30 inside 13:00–14:00 must be held")
	}
	// Equal start and end means no quiet time.
	if _, held, _ := HoldUntil(at(23, 0), "22:00", "22:00", loc); held {
		t.Fatal("equal bounds must disable quiet hours")
	}
	if _, _, err := HoldUntil(at(1, 0), "25:00", "07:00", loc); err == nil {
		t.Fatal("bad time must error")
	}
}
