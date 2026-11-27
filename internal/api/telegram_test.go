package api

import (
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t0mer/mashov2go/internal/connector"
)

const botTok = "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func fakeBotAPI(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			_, _ = io.WriteString(w, `{"ok":true,"result":{"username":"mashov_family_bot"}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestTelegramLinkIssuesOneTimeDeepLink(t *testing.T) {
	bot := fakeBotAPI(t)
	defer bot.Close()
	srv, h := newServer(t)
	srv.TelegramAPIBase = bot.URL
	cookie, csrf := signIn(t, h)
	adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/connectors", map[string]any{
		"type": "telegram", "name": "bot", "config": map[string]any{"bot_token": botTok},
	})
	kid := kidID(t, h, cookie, csrf)

	rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children/"+kid+"/telegram-link", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("link: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec.Body.String())
	url, _ := got["url"].(string)
	if !strings.HasPrefix(url, "https://t.me/mashov_family_bot?start=") {
		t.Fatalf("url = %q", url)
	}
	token := strings.TrimPrefix(url, "https://t.me/mashov_family_bot?start=")
	if _, err := hex.DecodeString(token); err != nil || len(token) != 32 {
		t.Fatalf("token is not a 32-hex bind token: %q", token)
	}
	if _, ok := connector.ParseStart("/start " + token); !ok {
		t.Fatal("the link's token is not accepted by the bot's /start parser")
	}

	// The link binds the chat once; the stored token is the hash, so the bot's
	// plaintext value is what matches.
	if _, err := srv.Store.ConsumeBindToken(t.Context(), token, 555, srv.now()); err != nil {
		t.Fatalf("bind with the link token: %v", err)
	}
	if _, err := srv.Store.ConsumeBindToken(t.Context(), token, 666, srv.now()); err == nil {
		t.Fatal("link token worked twice")
	}
}

func TestTelegramLinkNeedsABot(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	kid := kidID(t, h, cookie, csrf)
	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children/"+kid+"/telegram-link", nil); rec.Code != http.StatusConflict {
		t.Fatalf("no bot: %d, want 409", rec.Code)
	}
	if rec := adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/children/999/telegram-link", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown member: %d, want 404", rec.Code)
	}
}
