package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultGreenAPIURL is used when the connector config leaves api_url blank.
const DefaultGreenAPIURL = "https://api.green-api.com"

var (
	instanceRe = regexp.MustCompile(`^[0-9]{1,12}$`)
	phoneRe    = regexp.MustCompile(`^[0-9]{8,15}$`)
	tokenRe    = regexp.MustCompile(`^[\x21-\x7e]{10,200}$`) // printable, no whitespace
)

// GreenAPIConfig is the stored configuration of a Green-API WhatsApp connector.
// Values are trimmed by NewGreenAPI; whitespace in the token or instance id
// corrupts the request path, which is the most common cause of 400s.
type GreenAPIConfig struct {
	APIURL     string `json:"api_url"`
	InstanceID string `json:"instance_id"`
	Token      string `json:"token"`
}

// GreenAPI sends WhatsApp messages through a Green-API instance.
type GreenAPI struct {
	base     *url.URL
	instance string
	token    string
	hc       *http.Client
}

// NewGreenAPI validates cfg and returns a connector. hc may be nil; tests
// inject an httptest client. The API URL must be https (loopback allowed for tests).
func NewGreenAPI(cfg GreenAPIConfig, hc *http.Client) (*GreenAPI, error) {
	cfg.APIURL = strings.TrimSpace(cfg.APIURL)
	cfg.InstanceID = strings.TrimSpace(cfg.InstanceID)
	cfg.Token = strings.TrimSpace(cfg.Token)
	if cfg.APIURL == "" {
		cfg.APIURL = DefaultGreenAPIURL
	}
	base, err := url.Parse(cfg.APIURL)
	if err != nil || base.Host == "" {
		return nil, errors.New("green-api: api_url is not a valid URL")
	}
	if base.Scheme != "https" && !isLoopback(base) {
		return nil, errors.New("green-api: api_url must use https")
	}
	if !instanceRe.MatchString(cfg.InstanceID) {
		return nil, errors.New("green-api: instance id must be digits")
	}
	if !tokenRe.MatchString(cfg.Token) {
		return nil, errors.New("green-api: token has an unexpected format")
	}
	// Copy the client so the caller's client is not changed. Never follow
	// redirects: the token is part of the URL path.
	c := &http.Client{Timeout: 30 * time.Second}
	if hc != nil {
		cp := *hc
		c = &cp
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	hc = c
	return &GreenAPI{base: base, instance: cfg.InstanceID, token: cfg.Token, hc: hc}, nil
}

// Type implements Connector.
func (g *GreenAPI) Type() string { return "greenapi" }

// ChatID converts a phone (E.164 digits, optional leading +) to a Green-API chat id.
func ChatID(phone string) (string, error) {
	p := strings.TrimPrefix(strings.TrimSpace(phone), "+")
	if !phoneRe.MatchString(p) {
		return "", permanent("green-api: phone must be international digits, got %d characters", len(p))
	}
	return p + "@c.us", nil
}

// SendTo implements Connector.
func (g *GreenAPI) SendTo(ctx context.Context, address string, m Message) error {
	chatID, err := ChatID(address)
	if err != nil {
		return err
	}
	body := map[string]string{"chatId": chatID, "message": m.Text}
	_, status, hdr, err := g.call(ctx, http.MethodPost, "sendMessage", body)
	if err != nil {
		return transient("green-api sendMessage: %v", err)
	}
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusTooManyRequests:
		return &SendError{RetryAfter: retryAfter(hdr), Err: fmt.Errorf("green-api sendMessage: HTTP 429")}
	case status >= 500:
		return transient("green-api sendMessage: HTTP %d", status)
	default:
		return permanent("green-api sendMessage: HTTP %d", status)
	}
}

// Health implements Connector: the instance must report "authorized".
func (g *GreenAPI) Health(ctx context.Context) Status {
	raw, status, _, err := g.call(ctx, http.MethodGet, "getStateInstance", nil)
	if err != nil || status != http.StatusOK {
		return StatusDown
	}
	var st struct {
		StateInstance string `json:"stateInstance"`
	}
	if json.Unmarshal(raw, &st) != nil {
		return StatusDown
	}
	switch st.StateInstance {
	case "authorized":
		return StatusOK
	case "notAuthorized":
		return StatusPairingRequired
	default:
		return StatusDown
	}
}

// Close implements Connector.
func (g *GreenAPI) Close() error {
	g.hc.CloseIdleConnections()
	return nil
}

// call performs one Green-API method. The token appears only in the URL path,
// which is never placed in an error message.
func (g *GreenAPI) call(ctx context.Context, method, name string, body any) ([]byte, int, http.Header, error) {
	u := *g.base
	u.Path = strings.TrimSuffix(g.base.Path, "/") + "/waInstance" + g.instance + "/" + name + "/" + g.token
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
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.hc.Do(req)
	if err != nil {
		// url.Error embeds the full URL, which contains the token; report only the kind.
		return nil, 0, nil, errors.New("request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, nil, errors.New("reading response")
	}
	return raw, resp.StatusCode, resp.Header, nil
}

// retryAfter reads a Retry-After header in seconds; 0 when absent or invalid.
func retryAfter(h http.Header) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && n > 0 && n <= 3600 {
		return time.Duration(n) * time.Second
	}
	return 0
}

func isLoopback(u *url.URL) bool {
	h := u.Hostname()
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}
