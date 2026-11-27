package connector

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultTelegramBase is the Bot API root.
const DefaultTelegramBase = "https://api.telegram.org/"

// telegramMaxText is the Bot API limit for sendMessage text, in characters.
const telegramMaxText = 4096

// bindTTL is how long a deep-link binding token stays valid.
const bindTTL = 24 * time.Hour

var botTokenRe = regexp.MustCompile(`^[0-9]{5,15}:[A-Za-z0-9_-]{20,100}$`)

// Telegram sends messages through a Telegram bot.
type Telegram struct {
	base  *url.URL
	token string
	hc    *http.Client
}

// NewTelegram validates the bot token and returns a connector. base may be empty
// (Bot API root); tests pass an httptest URL.
func NewTelegram(token, base string, hc *http.Client) (*Telegram, error) {
	token = strings.TrimSpace(token)
	if !botTokenRe.MatchString(token) {
		return nil, errors.New("telegram: bot token has an unexpected format")
	}
	if base == "" {
		base = DefaultTelegramBase
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, errors.New("telegram: base url is not valid")
	}
	if u.Scheme != "https" && !isLoopback(u) {
		return nil, errors.New("telegram: base url must use https")
	}
	c := &http.Client{Timeout: 30 * time.Second}
	if hc != nil {
		cp := *hc
		c = &cp
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Telegram{base: u, token: token, hc: c}, nil
}

// Type implements Connector.
func (t *Telegram) Type() string { return "telegram" }

// SendTo implements Connector. address is the chat id bound to the member.
func (t *Telegram) SendTo(ctx context.Context, address string, m Message) error {
	if strings.TrimSpace(address) == "" {
		return permanent("telegram: no chat bound")
	}
	// Cutting HTML mid-tag makes Telegram reject the message, so an overlong
	// HTML body falls back to the escaped plain text.
	text := m.HTML
	if text == "" || utf8.RuneCountInString(text) > telegramMaxText {
		text = truncateRunes(html.EscapeString(m.Text), telegramMaxText)
	}
	body := map[string]any{"chat_id": address, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true}
	raw, status, err := t.call(ctx, "sendMessage", body)
	if err != nil {
		return transient("telegram sendMessage: %v", err)
	}
	if status == http.StatusOK {
		return nil
	}
	var resp struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	_ = json.Unmarshal(raw, &resp)
	switch {
	case status == http.StatusTooManyRequests:
		d := time.Duration(resp.Parameters.RetryAfter) * time.Second
		if d <= 0 || d > time.Hour {
			d = 0
		}
		return &SendError{RetryAfter: d, Err: fmt.Errorf("telegram sendMessage: HTTP 429")}
	case status >= 500:
		return transient("telegram sendMessage: HTTP %d", status)
	default:
		// 400 (chat not found / bad request) and 403 (bot blocked) do not recover on retry.
		return permanent("telegram sendMessage: HTTP %d", status)
	}
}

// Health implements Connector using getMe.
func (t *Telegram) Health(ctx context.Context) Status {
	_, status, err := t.call(ctx, "getMe", nil)
	if err != nil || status != http.StatusOK {
		return StatusDown
	}
	return StatusOK
}

// Username returns the bot's username without @ (getMe), used to build t.me
// deep links. The result is checked against Telegram's username rules because it
// is placed in a URL.
func (t *Telegram) Username(ctx context.Context) (string, error) {
	raw, status, err := t.call(ctx, "getMe", nil)
	if err != nil {
		return "", transient("telegram getMe: %v", err)
	}
	if status != http.StatusOK {
		return "", transient("telegram getMe: HTTP %d", status)
	}
	var resp struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &resp) != nil || !resp.OK || !botUsernameRe.MatchString(resp.Result.Username) {
		return "", transient("telegram getMe: bad response")
	}
	return resp.Result.Username, nil
}

var botUsernameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`)

// Close implements Connector.
func (t *Telegram) Close() error {
	t.hc.CloseIdleConnections()
	return nil
}

// Update is the subset of a Bot API update used for member binding.
type Update struct {
	ID     int64
	ChatID int64
	Text   string
}

// Updates long-polls getUpdates from offset. It returns the updates and the
// offset to pass next time (one past the highest id seen).
func (t *Telegram) Updates(ctx context.Context, offset int64, timeoutSec int) ([]Update, int64, error) {
	if timeoutSec < 0 || timeoutSec > 50 {
		timeoutSec = 0
	}
	raw, status, err := t.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeoutSec, "allowed_updates": []string{"message"},
	})
	if err != nil {
		return nil, offset, transient("telegram getUpdates: %v", err)
	}
	if status != http.StatusOK {
		return nil, offset, transient("telegram getUpdates: HTTP %d", status)
	}
	var resp struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  *struct {
				Text string `json:"text"`
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || !resp.OK {
		return nil, offset, transient("telegram getUpdates: bad response")
	}
	var out []Update
	next := offset
	for _, u := range resp.Result {
		if u.UpdateID+1 > next {
			next = u.UpdateID + 1
		}
		if u.Message == nil {
			continue
		}
		out = append(out, Update{ID: u.UpdateID, ChatID: u.Message.Chat.ID, Text: u.Message.Text})
	}
	return out, next, nil
}

func (t *Telegram) call(ctx context.Context, method string, body any) ([]byte, int, error) {
	u := *t.base
	u.Path = strings.TrimSuffix(t.base.Path, "/") + "/bot" + t.token + "/" + method
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), rdr)
	if err != nil {
		return nil, 0, errors.New("building request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := t.hc.Do(req)
	if err != nil {
		// url.Error embeds the URL, which contains the bot token.
		return nil, 0, errors.New("request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, errors.New("reading response")
	}
	return raw, resp.StatusCode, nil
}

// BindToken is a one-time token embedded in a t.me deep link. The member opens
// https://t.me/<bot>?start=<Value>; the bot then binds that chat to the member.
type BindToken struct {
	Value     string
	ExpiresAt time.Time
}

// NewBindToken returns a random token that expires after 24 h.
func NewBindToken(now time.Time) (BindToken, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return BindToken{}, fmt.Errorf("generating bind token: %w", err)
	}
	return BindToken{Value: hex.EncodeToString(b), ExpiresAt: now.Add(bindTTL)}, nil
}

// Valid reports whether presented matches the token and it has not expired.
// The comparison is constant-time.
func (b BindToken) Valid(presented string, now time.Time) bool {
	if b.Value == "" || now.After(b.ExpiresAt) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(b.Value)) == 1
}

var startRe = regexp.MustCompile(`^/start(?:@\w+)?\s+([0-9a-f]{32})\s*$`)

// ParseStart extracts the token from a "/start <token>" message.
func ParseStart(text string) (string, bool) {
	m := startRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// truncateRunes cuts s to at most n runes without splitting a UTF-8 sequence.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
