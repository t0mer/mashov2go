package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// OpenWA request shapes follow the OpenWA REST API as used by the Home
// Assistant integration (ha-openwa-notify) and the n8n node (n8n-nodes-openwa):
//   GET  /api/sessions/{id}                      health: status "ready"
//   POST /api/sessions/{id}/messages/send-text   body {"chatId", "text"}
// The API key travels only in the X-API-Key header.

const (
	openwaSessionReady = "ready"
	openwaPacingCode   = "SEND_PACING_LIMITED"
	openwaMaxRetryWait = time.Hour
)

var (
	openwaKeyRe      = regexp.MustCompile(`^owa_k1_[0-9a-f]{64}$`)
	openwaInstanceRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	chatSuffixRe     = regexp.MustCompile(`@(c\.us|g\.us|lid)$`)
	nonDigitRe       = regexp.MustCompile(`\D`)
)

// openwaChatID turns a phone into an OpenWA chat id. Separators are dropped, as
// in the reference integration; a local format (leading 0) is rejected.
func openwaChatID(phone string) (string, error) {
	digits := nonDigitRe.ReplaceAllString(phone, "")
	if len(digits) < 8 || len(digits) > 15 || strings.HasPrefix(digits, "0") {
		return "", permanent("openwa: phone must be international digits")
	}
	return digits + "@c.us", nil
}

// OpenWAConfig is the stored configuration of an OpenWA connector.
// InstanceID is the OpenWA session id.
type OpenWAConfig struct {
	BaseURL    string `json:"base_url"`
	InstanceID string `json:"instance_id"`
	APIKey     string `json:"api_key"`
}

// OpenWA sends WhatsApp messages through an OpenWA gateway session.
type OpenWA struct {
	base    *url.URL
	session string
	key     string
	hc      *http.Client
}

// NewOpenWA validates cfg and returns a connector. A trailing /api or /api/docs in
// base_url is removed, as the reference integration does. The gateway must use
// https (loopback allowed for tests). hc may be nil.
func NewOpenWA(cfg OpenWAConfig, hc *http.Client) (*OpenWA, error) {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.InstanceID = strings.TrimSpace(cfg.InstanceID)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)

	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Host == "" {
		return nil, errors.New("openwa: base_url is not a valid URL")
	}
	if base.Scheme != "https" && !isLoopback(base) {
		return nil, errors.New("openwa: base_url must use https")
	}
	base.RawQuery, base.Fragment = "", ""
	p := strings.TrimSuffix(strings.TrimSuffix(strings.TrimRight(base.Path, "/"), "/docs"), "/api")
	base.Path = strings.TrimRight(p, "/")
	base.RawPath = ""

	if !openwaInstanceRe.MatchString(cfg.InstanceID) {
		return nil, errors.New("openwa: instance id has an unexpected format")
	}
	if !openwaKeyRe.MatchString(cfg.APIKey) {
		return nil, errors.New("openwa: api key has an unexpected format")
	}
	// Copy the client; never follow redirects, since the key is a header.
	c := &http.Client{Timeout: 30 * time.Second}
	if hc != nil {
		cp := *hc
		c = &cp
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &OpenWA{base: base, session: cfg.InstanceID, key: cfg.APIKey, hc: c}, nil
}

// Type implements Connector.
func (o *OpenWA) Type() string { return "openwa" }

// SendTo implements Connector. address is a phone (E.164 digits) or a chat id
// that already ends in @c.us, @lid or @g.us. OpenWA takes plain text only.
func (o *OpenWA) SendTo(ctx context.Context, address string, m Message) error {
	chatID := strings.TrimSpace(address)
	if !chatSuffixRe.MatchString(chatID) {
		var err error
		if chatID, err = openwaChatID(address); err != nil {
			return err
		}
	}
	body := map[string]string{"chatId": chatID, "text": m.Text}
	raw, status, hdr, err := o.call(ctx, http.MethodPost, "/messages/send-text", body)
	if err != nil {
		return transient("openwa send-text: %v", err)
	}
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusTooManyRequests:
		wait := retryAfter(hdr)
		var b struct {
			Code           string `json:"code"`
			RetryAfterSecs *int   `json:"retryAfterSeconds"`
		}
		if json.Unmarshal(raw, &b) == nil && b.Code == openwaPacingCode && b.RetryAfterSecs != nil && *b.RetryAfterSecs > 0 {
			wait = time.Duration(*b.RetryAfterSecs) * time.Second
		}
		if wait > openwaMaxRetryWait {
			wait = openwaMaxRetryWait
		}
		return &SendError{RetryAfter: wait, Err: errors.New("openwa send-text: HTTP 429")}
	case status >= 500, status == http.StatusConflict:
		return transient("openwa send-text: HTTP %d", status)
	default:
		// 400/401/403/404/413 and other client errors will not succeed on retry.
		return permanent("openwa send-text: HTTP %d", status)
	}
}

// Health implements Connector: the session must report status "ready".
func (o *OpenWA) Health(ctx context.Context) Status {
	raw, status, _, err := o.call(ctx, http.MethodGet, "", nil)
	if err != nil || status != http.StatusOK {
		return StatusDown
	}
	var s struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return StatusDown
	}
	if s.Status == openwaSessionReady {
		return StatusOK
	}
	return StatusDown
}

// Close implements Connector.
func (o *OpenWA) Close() error {
	o.hc.CloseIdleConnections()
	return nil
}

// call performs one request under /api/sessions/{id}{suffix}. Errors never carry
// the URL or the key.
func (o *OpenWA) call(ctx context.Context, method, suffix string, body any) ([]byte, int, http.Header, error) {
	u := *o.base
	u.Path = o.base.Path + "/api/sessions/" + url.PathEscape(o.session) + suffix
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return nil, 0, nil, errors.New("building request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", o.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := o.hc.Do(req)
	if err != nil {
		return nil, 0, nil, errors.New("request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, nil, errors.New("reading response")
	}
	return raw, resp.StatusCode, resp.Header, nil
}
