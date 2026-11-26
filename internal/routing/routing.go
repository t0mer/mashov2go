// Package routing decides who receives an event and on which channel
// (CLAUDE.md §6.3). It is pure: callers load members and preferences.
package routing

import "github.com/t0mer/mashov2go/internal/family"

// Audience says whether a target is the child or a parent. Parents get the
// compact summary for daily_plan (CLAUDE.md §7).
const (
	AudienceChild  = "child"
	AudienceParent = "parent"
)

// Input is everything needed to route one event for one child.
type Input struct {
	Category  string
	Child     family.Member
	Overrides map[string]family.Recipient // stored per-child recipients; missing categories use defaults
	Parents   []family.Member             // every member with role parent (inactive ones are skipped)
	Muted     map[int64]bool              // parent member IDs muted for this category
}

// Target is one recipient of the event.
type Target struct {
	Member   family.Member
	Audience string
	Channel  string // channel to use; empty when SkipReason is set
	Fallback string // fallback channel, may be empty
	// SkipReason is set when the member cannot be reached (CLAUDE.md §6.3, skipped_no_channel).
	SkipReason string
}

// Recipient returns the effective recipient for a category.
func Recipient(category string, overrides map[string]family.Recipient) family.Recipient {
	if r, ok := overrides[category]; ok && family.ValidRecipient(r) {
		return r
	}
	if r, ok := family.DefaultRecipients[category]; ok {
		return r
	}
	return family.ToOff
}

// Route returns the targets for one event. A target without a usable channel is
// still returned, with SkipReason set, so the UI can show a warning.
func Route(in Input) []Target {
	r := Recipient(in.Category, in.Overrides)
	var out []Target
	if r == family.ToChild || r == family.ToBoth {
		out = append(out, target(in.Child, AudienceChild))
	}
	if r == family.ToParents || r == family.ToBoth {
		for _, p := range in.Parents {
			if !p.Active || in.Muted[p.ID] {
				continue
			}
			out = append(out, target(p, AudienceParent))
		}
	}
	return out
}

func target(m family.Member, audience string) Target {
	t := Target{Member: m, Audience: audience, Channel: m.Channel, Fallback: m.FallbackChannel}
	if !m.Active {
		// Clear the channel too, so a caller that ignores SkipReason cannot send.
		t.Channel, t.Fallback, t.SkipReason = "", "", "inactive"
		return t
	}
	if !Usable(m, m.Channel) {
		if t.Fallback != "" && Usable(m, t.Fallback) {
			t.Channel = t.Fallback
			t.Fallback = ""
		} else {
			t.Channel = ""
			t.SkipReason = "no_channel"
		}
	}
	return t
}

// Usable reports whether a member has what a channel needs (CLAUDE.md §6.1).
func Usable(m family.Member, channel string) bool {
	switch channel {
	case family.ChannelWhatsApp:
		return m.WhatsAppPhone != ""
	case family.ChannelTelegram:
		return m.TelegramChatID != 0
	}
	return false
}
