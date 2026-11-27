package connector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewGreenAPIValidation(t *testing.T) {
	good := GreenAPIConfig{InstanceID: " 1101 ", Token: " abcdefghijklmnopqrstuvwxyz "}
	g, err := NewGreenAPI(good, nil)
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if g.instance != "1101" || g.token != "abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("values not trimmed: %q %q", g.instance, g.token)
	}
	cases := map[string]GreenAPIConfig{
		"http url":       {APIURL: "http://api.example.com", InstanceID: "1", Token: "abcdefghijk"},
		"letters in id":  {InstanceID: "11a", Token: "abcdefghijk"},
		"short token":    {InstanceID: "1", Token: "abc"},
		"space in token": {InstanceID: "1", Token: "abc defghijkl"},
	}
	for name, cfg := range cases {
		if _, err := NewGreenAPI(cfg, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestGreenAPISendText(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"idMessage":"X"}`)
	}))
	defer srv.Close()
	g, err := NewGreenAPI(GreenAPIConfig{APIURL: srv.URL, InstanceID: "1101", Token: "abcdefghijklmnop"}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SendTo(context.Background(), "+972501234567", Message{Text: "hi"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotPath != "/waInstance1101/sendMessage/abcdefghijklmnop" {
		t.Errorf("path = %s", gotPath)
	}
	if gotBody["chatId"] != "972501234567@c.us" || gotBody["message"] != "hi" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestGreenAPISendErrorClassification(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		header    string
		permanent bool
		retry     time.Duration
	}{
		{"bad request is permanent", http.StatusBadRequest, "", true, 0},
		{"server error is transient", http.StatusInternalServerError, "", false, 0},
		{"rate limit carries Retry-After", http.StatusTooManyRequests, "30", false, 30 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.header != "" {
					w.Header().Set("Retry-After", c.header)
				}
				w.WriteHeader(c.status)
			}))
			defer srv.Close()
			g, _ := NewGreenAPI(GreenAPIConfig{APIURL: srv.URL, InstanceID: "1", Token: "abcdefghijk"}, srv.Client())
			err := g.SendTo(context.Background(), "972501234567", Message{Text: "x"})
			if err == nil {
				t.Fatal("expected error")
			}
			if IsPermanent(err) != c.permanent {
				t.Errorf("permanent = %v", IsPermanent(err))
			}
			if RetryAfterOf(err) != c.retry {
				t.Errorf("retry = %v", RetryAfterOf(err))
			}
			if strings.Contains(err.Error(), "abcdefghijk") {
				t.Errorf("error leaks token: %v", err)
			}
		})
	}
}

func TestGreenAPIRejectsBadPhone(t *testing.T) {
	g, _ := NewGreenAPI(GreenAPIConfig{InstanceID: "1", Token: "abcdefghijk"}, nil)
	err := g.SendTo(context.Background(), "050-123", Message{Text: "x"})
	if !IsPermanent(err) {
		t.Fatalf("malformed phone should be permanent, got %v", err)
	}
}

func TestGreenAPIHealth(t *testing.T) {
	states := map[string]Status{"authorized": StatusOK, "notAuthorized": StatusPairingRequired, "blocked": StatusDown}
	for state, want := range states {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"stateInstance":"`+state+`"}`)
		}))
		g, _ := NewGreenAPI(GreenAPIConfig{APIURL: srv.URL, InstanceID: "1", Token: "abcdefghijk"}, srv.Client())
		if got := g.Health(context.Background()); got != want {
			t.Errorf("state %s: got %s want %s", state, got, want)
		}
		srv.Close()
	}
}

func TestTelegramSendHTMLAndEscape(t *testing.T) {
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	tg, err := NewTelegram("123456:abcdefghijklmnopqrstuvwx", srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.SendTo(context.Background(), "42", Message{Text: "a < b & c"}); err != nil {
		t.Fatal(err)
	}
	if path != "/bot123456:abcdefghijklmnopqrstuvwx/sendMessage" {
		t.Errorf("path = %s", path)
	}
	if body["text"] != "a &lt; b &amp; c" || body["parse_mode"] != "HTML" {
		t.Errorf("body = %v", body)
	}
}

func TestTelegramRetryAfterAndPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") && r.Header.Get("X-Test") == "" {
			// Body decides the response: chat 1 is rate limited, chat 2 is blocked.
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["chat_id"] == "1" {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"ok":false,"parameters":{"retry_after":12}}`)
				return
			}
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	tg, _ := NewTelegram("123456:abcdefghijklmnopqrstuvwx", srv.URL, srv.Client())
	err := tg.SendTo(context.Background(), "1", Message{Text: "x"})
	if RetryAfterOf(err) != 12*time.Second || IsPermanent(err) {
		t.Errorf("429: retry=%v permanent=%v", RetryAfterOf(err), IsPermanent(err))
	}
	err = tg.SendTo(context.Background(), "2", Message{Text: "x"})
	if !IsPermanent(err) {
		t.Errorf("403 should be permanent, got %v", err)
	}
}

func TestTelegramUpdatesOffset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true,"result":[
			{"update_id":10,"message":{"text":"/start `+strings.Repeat("a", 32)+`","chat":{"id":555}}},
			{"update_id":11}]}`)
	}))
	defer srv.Close()
	tg, _ := NewTelegram("123456:abcdefghijklmnopqrstuvwx", srv.URL, srv.Client())
	ups, next, err := tg.Updates(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != 12 || len(ups) != 1 || ups[0].ChatID != 555 {
		t.Fatalf("next=%d ups=%+v", next, ups)
	}
	if tok, ok := ParseStart(ups[0].Text); !ok || tok != strings.Repeat("a", 32) {
		t.Errorf("ParseStart failed: %q %v", tok, ok)
	}
}

func TestTelegramTokenNotInError(t *testing.T) {
	tg, _ := NewTelegram("123456:abcdefghijklmnopqrstuvwx", "https://127.0.0.1:1/", nil)
	err := tg.SendTo(context.Background(), "1", Message{Text: "x"})
	if err == nil {
		t.Fatal("expected connection error")
	}
	if strings.Contains(err.Error(), "abcdefghijklmnopqrstuvwx") {
		t.Errorf("error leaks token: %v", err)
	}
	var se *SendError
	if !errors.As(err, &se) || se.Permanent {
		t.Errorf("connection failure should be transient")
	}
}

func TestTelegramRejectsBadToken(t *testing.T) {
	for _, tok := range []string{"", "notoken", "123:short", "123456:abc def ghijklmnopqrstu"} {
		if _, err := NewTelegram(tok, "", nil); err == nil {
			t.Errorf("token %q accepted", tok)
		}
	}
}

func TestBindToken(t *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	tok, err := NewBindToken(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok.Value) != 32 {
		t.Fatalf("token length %d", len(tok.Value))
	}
	if !tok.Valid(tok.Value, now.Add(23*time.Hour)) {
		t.Error("valid token rejected")
	}
	if tok.Valid(tok.Value, now.Add(25*time.Hour)) {
		t.Error("expired token accepted")
	}
	if tok.Valid("0", now) || (BindToken{}).Valid("", now) {
		t.Error("wrong or empty token accepted")
	}
}

func TestParseStart(t *testing.T) {
	ok := strings.Repeat("0f", 16)
	if tok, good := ParseStart("/start " + ok); !good || tok != ok {
		t.Error("plain /start rejected")
	}
	if _, good := ParseStart("/start@mybot " + ok); !good {
		t.Error("/start@bot form rejected")
	}
	for _, bad := range []string{"/start", "/start zz", "hello " + ok, "/start " + ok + " extra"} {
		if _, good := ParseStart(bad); good {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("שלום", 2); got != "של" {
		t.Errorf("got %q", got)
	}
}

func TestTelegramUsernameFromGetMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getMe") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"username":"mashov_family_bot"}}`)
	}))
	defer srv.Close()
	tg, err := NewTelegram("123456:abcdefghijklmnopqrstuvwx", srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	name, err := tg.Username(context.Background())
	if err != nil || name != "mashov_family_bot" {
		t.Fatalf("username = %q, %v", name, err)
	}
}

func TestTelegramUsernameRejectsUnsafeValues(t *testing.T) {
	for _, bad := range []string{`"a/b?x=1"`, `"1startsWithDigit"`, `"ab"`, `""`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"ok":true,"result":{"username":`+bad+`}}`)
		}))
		tg, _ := NewTelegram("123456:abcdefghijklmnopqrstuvwx", srv.URL, srv.Client())
		if _, err := tg.Username(context.Background()); err == nil {
			t.Errorf("username %s accepted", bad)
		}
		srv.Close()
	}
}
