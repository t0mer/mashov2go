package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/t0mer/mashov2go/internal/connector"
	"github.com/t0mer/mashov2go/internal/store"
)

// telegramLink creates a one-time bind link for a member: the member opens the
// link and sends /start to the bot, which binds their chat (CLAUDE.md §8.1).
// The token is returned once and stored only as a hash.
func (s *Server) telegramLink(w http.ResponseWriter, r *http.Request) {
	m, ok := s.loadAnyMember(w, r)
	if !ok {
		return
	}
	bot, err := s.firstTelegramBot(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		problem(w, http.StatusConflict, "No Telegram bot", "add an enabled Telegram connector first")
		return
	}
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "loading the Telegram connector failed")
		return
	}
	defer bot.Close()
	tg, ok := bot.(*connector.Telegram)
	if !ok {
		problem(w, http.StatusInternalServerError, "Internal error", "the Telegram connector is not usable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	username, err := tg.Username(ctx)
	if err != nil {
		problem(w, http.StatusBadGateway, "Telegram unreachable", "the bot could not be reached; try again later")
		return
	}
	now := s.now()
	tok, err := connector.NewBindToken(now)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "creating the link failed")
		return
	}
	if err := s.Store.CreateBindToken(r.Context(), m.ID, tok.Value, tok.ExpiresAt); err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "saving the link failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"url":       "https://t.me/" + username + "?start=" + tok.Value,
		"expiresAt": tok.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// firstTelegramBot opens the first enabled Telegram connector, as the outbox does.
func (s *Server) firstTelegramBot(ctx context.Context) (connector.Connector, error) {
	list, err := s.Store.ListConnectors(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		if c.Type == "telegram" && c.Enabled {
			return s.openConnector(ctx, c)
		}
	}
	return nil, store.ErrNotFound
}
