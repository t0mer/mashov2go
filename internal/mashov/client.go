// Package mashov is an unofficial, parent-scope client for the Mashov
// (משו"ב) web API as used by com.mashov.main 3.8.72. All Mashov-specific
// knowledge lives in this package. See CLAUDE.md §2–§4.
package mashov

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the Mashov API root.
const DefaultBaseURL = "https://web.mashov.info/api/"

// DefaultMinGap is the minimum spacing between two requests on one session.
const DefaultMinGap = 750 * time.Millisecond

// ErrSessionExpired is returned when Mashov rejects the session (401/419/440).
var ErrSessionExpired = errors.New("mashov: session expired")

// ErrRateLimited is returned on HTTP 429. RetryAfter carries the server hint.
var ErrRateLimited = errors.New("mashov: rate limited")

// HTTPError is returned for any non-2xx response that is not handled above.
type HTTPError struct {
	Method string
	Path   string
	Status int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("mashov: %s %s: HTTP %d", e.Method, e.Path, e.Status)
}

// Options configures a Client. Zero values select the documented defaults.
type Options struct {
	BaseURL   string
	MinGap    time.Duration
	Profile   Profile
	Transport http.RoundTripper // optional; tests inject httptest transports
	// MaxRetries applies to GET requests on 429/5xx (default 3).
	MaxRetries int
	// Backoff is the base delay for retries (default 2s, doubled per attempt, jittered).
	Backoff time.Duration
}

// Client is one Mashov session: one cookie jar, one CSRF token, one
// serialized request queue. Create one Client per (account, school, year).
type Client struct {
	base       *url.URL
	hc         *http.Client
	profile    Profile
	minGap     time.Duration
	maxRetries int
	backoff    time.Duration

	// mu serializes requests so a session never has more than one in flight.
	mu       sync.Mutex
	lastSent time.Time
	csrf     string

	// reauth, if set, is invoked once on a GET that hits session expiry;
	// the GET is then replayed once. See SetReauth.
	reauth func(ctx context.Context) error
}

// NewClient builds a Client with its own cookie jar.
func NewClient(opts Options) (*Client, error) {
	raw := opts.BaseURL
	if raw == "" {
		raw = DefaultBaseURL
	}
	base, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing base url: %w", err)
	}
	if base.Scheme != "https" && !isLoopback(base) {
		return nil, fmt.Errorf("mashov base url must be https: %s", raw)
	}
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", err)
	}
	c := &Client{
		base:       base,
		hc:         &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: opts.Transport},
		profile:    opts.Profile,
		minGap:     opts.MinGap,
		maxRetries: opts.MaxRetries,
		backoff:    opts.Backoff,
	}
	if c.profile.Name == "" {
		c.profile = AndroidProfile
	}
	if c.minGap == 0 {
		c.minGap = DefaultMinGap
	}
	if c.maxRetries == 0 {
		c.maxRetries = 3
	}
	if c.backoff == 0 {
		c.backoff = 2 * time.Second
	}
	c.hc.CheckRedirect = sameOriginRedirects(base)
	return c, nil
}

// sameOriginRedirects refuses any redirect that leaves the configured origin.
// Go would otherwise re-send custom headers (X-Csrf-Token, DevicePass) and a
// replayed login body to whatever host the server points at.
func sameOriginRedirects(base *url.URL) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("mashov: too many redirects")
		}
		if req.URL.Host != base.Host || (req.URL.Scheme != "https" && !isLoopback(req.URL)) {
			return fmt.Errorf("mashov: refusing redirect to %s", req.URL.Redacted())
		}
		return nil
	}
}

// SetReauth registers the re-login hook used on session expiry.
func (c *Client) SetReauth(fn func(ctx context.Context) error) { c.reauth = fn }

// CSRF returns the current X-Csrf-Token value ("" before login).
func (c *Client) CSRF() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.csrf
}

// response is the fully-read result of one HTTP exchange.
type response struct {
	status int
	header http.Header
	body   []byte
}

// send performs one request under the session lock, honouring the minimum gap.
// It never retries; callers decide.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, body any, extra http.Header) (*response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if wait := c.minGap - time.Since(c.lastSent); !c.lastSent.IsZero() && wait > 0 {
		if err := sleepCtx(ctx, wait); err != nil {
			return nil, err
		}
	}
	c.lastSent = time.Now()

	u := *c.base
	u.Path = c.base.Path + strings.TrimPrefix(path, "/")
	if query != nil {
		u.RawQuery = query.Encode()
	}

	var rdr io.Reader
	if body != nil {
		b, err := marshalBody(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", c.profile.UserAgent)
	if c.csrf != "" {
		req.Header.Set("X-Csrf-Token", c.csrf)
	}
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("reading %s %s: %w", method, path, err)
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

// get runs a GET with retry on 429/5xx (jittered exponential backoff) and one
// re-auth + replay on session expiry (CLAUDE.md §3.3).
func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.getWith(ctx, path, query, true)
}

// getWith is get with control over whether session expiry may trigger re-auth.
// Probes and logout pass allowReauth=false so they never start a password login.
func (c *Client) getWith(ctx context.Context, path string, query url.Values, allowReauth bool) ([]byte, error) {
	reauthed := !allowReauth
	for attempt := 0; ; attempt++ {
		r, err := c.send(ctx, http.MethodGet, path, query, nil, nil)
		if err != nil {
			return nil, err
		}
		switch {
		case r.status >= 200 && r.status < 300:
			return r.body, nil
		case r.status == http.StatusUnauthorized, r.status == 419, r.status == 440:
			if c.reauth == nil || reauthed {
				return nil, ErrSessionExpired
			}
			reauthed = true
			if err := c.reauth(ctx); err != nil {
				return nil, fmt.Errorf("re-auth: %w", err)
			}
			attempt-- // the replay after re-auth does not consume a retry
		case r.status == http.StatusTooManyRequests || r.status >= 500:
			if attempt >= c.maxRetries {
				if r.status == http.StatusTooManyRequests {
					return nil, ErrRateLimited
				}
				return nil, &HTTPError{Method: http.MethodGet, Path: path, Status: r.status}
			}
			if err := sleepCtx(ctx, c.backoffFor(attempt)); err != nil {
				return nil, err
			}
		default:
			return nil, &HTTPError{Method: http.MethodGet, Path: path, Status: r.status}
		}
	}
}

// Get fetches a parent-scope JSON resource and returns the raw body.
// Query values are passed through as-is.
func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.get(ctx, path, query)
}

func (c *Client) backoffFor(attempt int) time.Duration {
	d := c.backoff << attempt
	// ±20 % jitter so parallel sessions do not synchronise their retries.
	jitter := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(d) * jitter)
}

func (c *Client) setCSRF(v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.csrf = v
}

func marshalBody(v any) ([]byte, error) {
	switch b := v.(type) {
	case []byte:
		return b, nil
	case string:
		return []byte(b), nil
	}
	return json.Marshal(v)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func isLoopback(u *url.URL) bool {
	h := u.Hostname()
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// SetCSRFForTest seeds the CSRF token. Tests only; production sets it via Login.
func (c *Client) SetCSRFForTest(v string) { c.setCSRF(v) }
