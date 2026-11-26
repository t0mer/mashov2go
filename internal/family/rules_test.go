package family

import "testing"

func validParent() Member {
	return Member{Role: RoleParent, Name: "P", Language: "he", Channel: ChannelWhatsApp,
		WhatsAppPhone: "972501234567", QuietStart: "22:30", QuietEnd: "06:30", PlanTime: "19:30"}
}

func TestValidMemberPasses(t *testing.T) {
	if err := validParent().Validate(); err != nil {
		t.Fatalf("valid member rejected: %v", err)
	}
}

func TestMemberRules(t *testing.T) {
	cases := map[string]func(*Member){
		"no phone on whatsapp":  func(m *Member) { m.WhatsAppPhone = "" },
		"telegram without chat": func(m *Member) { m.Channel = ChannelTelegram; m.WhatsAppPhone = "" },
		"bad role":              func(m *Member) { m.Role = "teacher" },
		"bad language":          func(m *Member) { m.Language = "fr" },
		"bad quiet time":        func(m *Member) { m.QuietStart = "9pm" },
		"empty name":            func(m *Member) { m.Name = "" },
	}
	for name, mut := range cases {
		m := validParent()
		mut(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestDefaultsCoverEveryCategory(t *testing.T) {
	for _, c := range Categories {
		if _, ok := DefaultRecipients[c]; !ok {
			t.Errorf("no default for %s", c)
		}
	}
	if DefaultRecipients[CatDailyPlan] != ToChild || DefaultRecipients[CatTimetableChange] != ToBoth {
		t.Fatal("defaults do not match CLAUDE.md §6.2")
	}
}
