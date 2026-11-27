// Package telegrambind completes Telegram bindings: it reads /start messages
// from the bot and binds the sender's chat to the member that owns the token
// (CLAUDE.md §8.1). It never sends messages itself.
package telegrambind

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/t0mer/mashov2go/internal/connector"
	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/store"
)

// Updater is the part of the Telegram connector this job needs.
type Updater interface {
	Updates(ctx context.Context, offset int64, timeoutSec int) ([]connector.Update, int64, error)
	SendTo(ctx context.Context, address string, m connector.Message) error
}

// TomorrowFunc renders a child's plan for the next school day. skip is the reason
// when there is no plan (CLAUDE.md §7).
type TomorrowFunc func(ctx context.Context, child family.Member) (text, skip string, err error)

// tomorrowCommands are the kids' commands that ask for tomorrow's plan.
var tomorrowCommands = map[string]bool{"/מחר": true, "/tomorrow": true}

// BotResolver returns the bot to poll and an ID that keys its update offset.
// ErrNoBot means no enabled Telegram bot exists yet, which is not an error.
type BotResolver func(ctx context.Context) (id int64, bot Updater, err error)

// ErrNoBot is returned by a BotResolver when no enabled Telegram bot exists.
var ErrNoBot = errors.New("telegrambind: no enabled telegram bot")

// Service consumes bind tokens from bot updates and answers the kids' commands.
type Service struct {
	Store *store.Store
	Bot   BotResolver
	Now   func() time.Time
	// Tomorrow answers /מחר for a bound child. Nil disables the commands.
	Tomorrow TomorrowFunc
	offsets  map[int64]int64
}

// RunOnce drains pending updates once and binds every valid /start token. It
// returns how many chats were bound. Unknown, used or expired tokens are ignored.
func (s *Service) RunOnce(ctx context.Context) (int, error) {
	id, bot, err := s.Bot(ctx)
	if errors.Is(err, ErrNoBot) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if s.offsets == nil {
		s.offsets = map[int64]int64{}
	}
	updates, next, err := bot.Updates(ctx, s.offsets[id], 0)
	if err != nil {
		return 0, err
	}
	bound := 0
	now := s.now()
	for _, u := range updates {
		if cmd := command(u.Text); cmd != "" {
			if err := s.answerCommand(ctx, bot, u.ChatID); err != nil {
				return bound, err
			}
			continue
		}
		token, ok := connector.ParseStart(u.Text)
		if !ok {
			continue
		}
		_, err := s.Store.ConsumeBindToken(ctx, token, u.ChatID, now)
		switch {
		case err == nil:
			bound++
		case errors.Is(err, store.ErrBindInvalid):
			// Ignored: the same message must not stop the others.
		default:
			return bound, fmt.Errorf("binding chat: %w", err)
		}
	}
	s.offsets[id] = next
	return bound, nil
}

// command returns the command word of a message ("/מחר"), without a bot suffix.
func command(text string) string {
	word := strings.TrimSpace(text)
	if i := strings.IndexAny(word, " \n"); i >= 0 {
		word = word[:i]
	}
	if i := strings.Index(word, "@"); i >= 0 {
		word = word[:i]
	}
	if tomorrowCommands[word] {
		return word
	}
	return ""
}

// answerCommand replies with tomorrow's plan when the chat belongs to a child.
// Chats that are not bound to a child get no reply.
func (s *Service) answerCommand(ctx context.Context, bot Updater, chatID int64) error {
	if s.Tomorrow == nil {
		return nil
	}
	child, err := s.Store.MemberByTelegramChat(ctx, chatID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && child.Role != family.RoleChild) {
		return nil
	}
	if err != nil {
		return err
	}
	reply := ""
	text, skip, err := s.Tomorrow(ctx, child)
	switch {
	case err != nil:
		reply = "לא ניתן לטעון את המערכת כרגע. נסו שוב מאוחר יותר."
	case skip != "":
		reply = "מחר אין לימודים 🎉"
	default:
		reply = text
	}
	return bot.SendTo(ctx, strconv.FormatInt(chatID, 10), connector.Message{Text: reply})
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// StoreBot returns a resolver that uses the first enabled Telegram connector in
// the store. It builds the client from the sealed config on each call, so a
// changed token takes effect without a restart.
func StoreBot(st *store.Store, hc *http.Client) BotResolver {
	return func(ctx context.Context) (int64, Updater, error) {
		list, err := st.ListConnectors(ctx)
		if err != nil {
			return 0, nil, err
		}
		for _, c := range list {
			if c.Type != "telegram" || !c.Enabled {
				continue
			}
			cfg, err := st.ConnectorConfig(ctx, c.ID)
			if err != nil {
				return 0, nil, err
			}
			built, err := connector.Build(c.Type, cfg, hc)
			if err != nil {
				return 0, nil, fmt.Errorf("building telegram connector %d: %w", c.ID, err)
			}
			u, ok := built.(Updater)
			if !ok {
				return 0, nil, fmt.Errorf("telegram connector %d cannot read updates", c.ID)
			}
			return c.ID, u, nil
		}
		return 0, nil, ErrNoBot
	}
}
