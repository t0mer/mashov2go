package family

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Member roles and channels.
const (
	RoleParent = "parent"
	RoleChild  = "child"

	ChannelWhatsApp = "whatsapp"
	ChannelTelegram = "telegram"
)

// Recipient is who receives a category for a child (CLAUDE.md §6.2).
type Recipient string

const (
	ToChild   Recipient = "child"
	ToParents Recipient = "parents"
	ToBoth    Recipient = "both"
	ToOff     Recipient = "off"
)

// Message categories (CLAUDE.md §6.2).
const (
	CatDailyPlan       = "daily_plan"
	CatHomework        = "homework"
	CatTimetableChange = "timetable_change"
	CatGrades          = "grades"
	CatBehave          = "behave"
	CatMaakav          = "maakav"
	CatMail            = "mail"
	CatSchoolNotice    = "school_notice"
	CatForms           = "forms"
	CatSystemAuth      = "system.auth"
)

// Categories lists every category in display order.
var Categories = []string{
	CatDailyPlan, CatHomework, CatTimetableChange, CatGrades, CatBehave,
	CatMaakav, CatMail, CatSchoolNotice, CatForms, CatSystemAuth,
}

// DefaultRecipients are the seeded defaults for a child (CLAUDE.md §6.2).
var DefaultRecipients = map[string]Recipient{
	CatDailyPlan:       ToChild,
	CatHomework:        ToChild,
	CatTimetableChange: ToBoth,
	CatGrades:          ToParents,
	CatBehave:          ToParents,
	CatMaakav:          ToParents,
	CatMail:            ToParents,
	CatSchoolNotice:    ToParents,
	CatForms:           ToParents,
	CatSystemAuth:      ToParents,
}

// ValidRecipient reports whether r is one of the four values.
func ValidRecipient(r Recipient) bool {
	switch r {
	case ToChild, ToParents, ToBoth, ToOff:
		return true
	}
	return false
}

// Member is a family member. Children are linked to a Mashov student; parents are not.
type Member struct {
	ID                   int64
	Role                 string
	Name                 string
	Language             string // he | en
	WhatsAppPhone        string // E.164 digits, empty when unset
	TelegramChatID       int64  // 0 when unbound
	Channel              string
	WhatsAppConnectorID  int64 // 0 uses the default connector
	FallbackChannel      string
	QuietStart           string // HH:MM
	QuietEnd             string // HH:MM
	PlanEnabled          bool
	PlanTime             string // HH:MM
	IncludeHomeworkToday bool
	Active               bool
	CreatedAt            time.Time
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// Validate enforces the rules of CLAUDE.md §6.1 before a member is saved.
// Phone normalisation happens in NormalizePhone; this checks the result.
func (m Member) Validate() error {
	var errs []error
	if m.Role != RoleParent && m.Role != RoleChild {
		errs = append(errs, fmt.Errorf("role must be parent or child, got %q", m.Role))
	}
	if m.Name == "" {
		errs = append(errs, errors.New("name is required"))
	}
	if m.Language != "he" && m.Language != "en" {
		errs = append(errs, fmt.Errorf("language must be he or en, got %q", m.Language))
	}
	if m.Channel != ChannelWhatsApp && m.Channel != ChannelTelegram {
		errs = append(errs, fmt.Errorf("channel must be whatsapp or telegram, got %q", m.Channel))
	}
	if m.FallbackChannel != "" && m.FallbackChannel != ChannelWhatsApp && m.FallbackChannel != ChannelTelegram {
		errs = append(errs, fmt.Errorf("fallback channel must be whatsapp or telegram, got %q", m.FallbackChannel))
	}
	if m.Channel == ChannelWhatsApp && m.WhatsAppPhone == "" {
		errs = append(errs, errors.New("a WhatsApp channel requires a phone number"))
	}
	if m.Channel == ChannelTelegram && m.TelegramChatID == 0 {
		errs = append(errs, errors.New("a Telegram channel requires a bound chat"))
	}
	for label, v := range map[string]string{"quiet start": m.QuietStart, "quiet end": m.QuietEnd, "plan time": m.PlanTime} {
		if !hhmm.MatchString(v) {
			errs = append(errs, fmt.Errorf("%s must be HH:MM, got %q", label, v))
		}
	}
	return errors.Join(errs...)
}
