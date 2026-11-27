package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/auth"
	"github.com/t0mer/mashov2go/internal/crypto"
	"github.com/t0mer/mashov2go/internal/settings"
	"github.com/t0mer/mashov2go/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const adminPass = "correct horse battery"

func newServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	auth.Cost = bcrypt.MinCost // keep the suite fast; production uses bcrypt.DefaultCost
	sealer, err := crypto.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "api.db"), sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc, err := settings.Load(t.Context(), st)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{Store: st, Settings: svc, Limiter: &auth.Limiter{Max: 3, Window: time.Hour}}
	return srv, srv.Handler()
}

// call sends one request and returns the response.
func call(h http.Handler, method, path string, body any, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.RemoteAddr = "192.0.2.10:5555"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("no session cookie in response (status %d)", rec.Code)
	return nil
}

func csrfOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.CSRFToken == "" {
		t.Fatalf("no csrf token: %s", rec.Body.String())
	}
	return out.CSRFToken
}

// signIn sets up the admin password and returns the session cookie and CSRF token.
func signIn(t *testing.T, h http.Handler) (*http.Cookie, string) {
	t.Helper()
	rec := call(h, http.MethodPost, "/api/v1/setup", map[string]string{"password": adminPass}, nil, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	return sessionCookie(t, rec), csrfOf(t, rec)
}

func TestSetupRunsOnceAndSignsIn(t *testing.T) {
	_, h := newServer(t)
	if rec := call(h, http.MethodGet, "/api/v1/setup", nil, nil, nil); !strings.Contains(rec.Body.String(), `"configured":false`) {
		t.Fatalf("fresh status: %s", rec.Body.String())
	}
	if rec := call(h, http.MethodPost, "/api/v1/setup", map[string]string{"password": "short"}, nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("short password: %d", rec.Code)
	}
	cookie, csrf := signIn(t, h)
	if cookie == nil || csrf == "" {
		t.Fatal("setup did not sign in")
	}
	if rec := call(h, http.MethodPost, "/api/v1/setup", map[string]string{"password": "another long password"}, nil, nil); rec.Code != http.StatusConflict {
		t.Fatalf("second setup: %d, want 409", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/setup", nil, nil, nil); !strings.Contains(rec.Body.String(), `"configured":true`) {
		t.Fatalf("configured status: %s", rec.Body.String())
	}
}

func TestLoginRateLimitsFailures(t *testing.T) {
	_, h := newServer(t)
	signIn(t, h)

	for i := 0; i < 3; i++ {
		if rec := call(h, http.MethodPost, "/api/v1/auth/login", map[string]string{"password": "wrong wrong wrong"}, nil, nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d, want 401", i, rec.Code)
		}
	}
	// Even the right password is refused once the address is blocked.
	if rec := call(h, http.MethodPost, "/api/v1/auth/login", map[string]string{"password": adminPass}, nil, nil); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked login: %d, want 429", rec.Code)
	}
}

func TestLoginRefusedBeforeSetup(t *testing.T) {
	_, h := newServer(t)
	if rec := call(h, http.MethodPost, "/api/v1/auth/login", map[string]string{"password": adminPass}, nil, nil); rec.Code != http.StatusConflict {
		t.Fatalf("login before setup: %d, want 409", rec.Code)
	}
}

func TestSessionChangesNeedCSRF(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	body := map[string]string{"name": "dash", "scope": "read"}

	if rec := call(h, http.MethodPost, "/api/v1/tokens", body, cookie, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("no CSRF: %d, want 403", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/tokens", body, cookie, map[string]string{"X-CSRF-Token": "wrong"}); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong CSRF: %d, want 403", rec.Code)
	}
	rec := call(h, http.MethodPost, "/api/v1/tokens", body, cookie, map[string]string{"X-CSRF-Token": csrf})
	if rec.Code != http.StatusCreated {
		t.Fatalf("with CSRF: %d %s", rec.Code, rec.Body.String())
	}
}

func TestReadTokenCanReadButNotChange(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	rec := call(h, http.MethodPost, "/api/v1/tokens", map[string]string{"name": "dash", "scope": "read"}, cookie,
		map[string]string{"X-CSRF-Token": csrf})
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("token not returned once: %s", rec.Body.String())
	}
	bearer := map[string]string{"Authorization": "Bearer " + out.Token}

	if rec := call(h, http.MethodGet, "/api/v1/auth/me", nil, nil, bearer); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"scope":"read"`) {
		t.Fatalf("read token on /me: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h, http.MethodGet, "/api/v1/tokens", nil, nil, bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("read token listing tokens: %d, want 403", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/tokens", map[string]string{"name": "x", "scope": "admin"}, nil, bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("read token creating tokens: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/auth/logout", nil, nil, bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("read token on admin route: %d, want 403", rec.Code)
	}
}

func TestRevokedTokenStopsWorking(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	rec := call(h, http.MethodPost, "/api/v1/tokens", map[string]string{"name": "ci", "scope": "admin"}, cookie,
		map[string]string{"X-CSRF-Token": csrf})
	var created struct {
		Token string `json:"token"`
		Info  struct {
			ID int64 `json:"id"`
		} `json:"info"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	bearer := map[string]string{"Authorization": "Bearer " + created.Token}
	if rec := call(h, http.MethodGet, "/api/v1/tokens", nil, nil, bearer); rec.Code != http.StatusOK {
		t.Fatalf("admin token listing: %d", rec.Code)
	}
	if rec := call(h, http.MethodDelete, "/api/v1/tokens/"+itoa(created.Info.ID), nil, cookie,
		map[string]string{"X-CSRF-Token": csrf}); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/auth/me", nil, nil, bearer); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d, want 401", rec.Code)
	}
	// The stored list keeps the revoked token and never shows its secret.
	list := call(h, http.MethodGet, "/api/v1/tokens", nil, cookie, nil)
	if strings.Contains(list.Body.String(), created.Token) || !strings.Contains(list.Body.String(), "revokedAt") {
		t.Fatalf("token listing: %s", list.Body.String())
	}
}

func TestLogoutEndsSession(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	if rec := call(h, http.MethodPost, "/api/v1/auth/logout", nil, cookie, map[string]string{"X-CSRF-Token": csrf}); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/auth/me", nil, cookie, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d, want 401", rec.Code)
	}
}

func TestPasswordChangeEndsOtherSessions(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	login := call(h, http.MethodPost, "/api/v1/auth/login", map[string]string{"password": adminPass}, nil, nil)
	other := sessionCookie(t, login)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	if rec := call(h, http.MethodPost, "/api/v1/settings/admin-password",
		map[string]string{"current": "wrong wrong wrong", "new": "a new long password"}, cookie, hdr); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current: %d, want 401", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/settings/admin-password",
		map[string]string{"current": adminPass, "new": "a new long password"}, cookie, hdr); rec.Code != http.StatusNoContent {
		t.Fatalf("change: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h, http.MethodGet, "/api/v1/auth/me", nil, cookie, nil); rec.Code != http.StatusOK {
		t.Fatalf("current session ended: %d", rec.Code)
	}
	if rec := call(h, http.MethodGet, "/api/v1/auth/me", nil, other, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other session survived: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/auth/login", map[string]string{"password": adminPass}, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password still accepted: %d", rec.Code)
	}
}

func TestProtectedRouteReturnsProblemJSON(t *testing.T) {
	_, h := newServer(t)
	rec := call(h, http.MethodGet, "/api/v1/tokens", nil, nil, nil)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status %d content-type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestClientIPTrustsForwardedHeaderOnlyFromTrustedProxy(t *testing.T) {
	srv := &Server{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	untrusted := httptest.NewRequest(http.MethodPost, "/", nil)
	untrusted.RemoteAddr = "203.0.113.7:4000"
	untrusted.Header.Set("X-Forwarded-For", "198.51.100.1")
	if got := srv.clientIP(untrusted); got != "203.0.113.7" {
		t.Fatalf("untrusted peer: key %q", got)
	}
	trusted := httptest.NewRequest(http.MethodPost, "/", nil)
	trusted.RemoteAddr = "127.0.0.1:4000"
	trusted.Header.Set("X-Forwarded-For", "198.51.100.1, 127.0.0.1")
	if got := srv.clientIP(trusted); got != "198.51.100.1" {
		t.Fatalf("trusted proxy: key %q", got)
	}
	// A client behind the proxy cannot pick its own key by sending a leading address.
	spoof := httptest.NewRequest(http.MethodPost, "/", nil)
	spoof.RemoteAddr = "127.0.0.1:4000"
	spoof.Header.Set("X-Forwarded-For", "10.99.99.99, 198.51.100.1")
	if got := srv.clientIP(spoof); got != "198.51.100.1" {
		t.Fatalf("spoofed leading address used: key %q", got)
	}
	// An untrusted client's header is ignored entirely.
	direct := httptest.NewRequest(http.MethodPost, "/", nil)
	direct.RemoteAddr = "203.0.113.7:4000"
	direct.Header.Set("X-Forwarded-For", "10.99.99.99")
	if got := srv.clientIP(direct); got != "203.0.113.7" {
		t.Fatalf("direct client header honoured: key %q", got)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestMeReturnsCSRFForSessionOnly(t *testing.T) {
	_, h := newServer(t)
	cookie, csrf := signIn(t, h)
	rec := call(h, http.MethodGet, "/api/v1/auth/me", nil, cookie, nil)
	if got := decodeMap(t, rec.Body.String()); got["csrfToken"] != csrf {
		t.Fatalf("me for session: %v", got)
	}
	tok := decodeMap(t, adminCall(h, cookie, csrf, http.MethodPost, "/api/v1/tokens",
		map[string]string{"name": "x", "scope": "read"}).Body.String())["token"].(string)
	rec = call(h, http.MethodGet, "/api/v1/auth/me", nil, nil, map[string]string{"Authorization": "Bearer " + tok})
	if strings.Contains(rec.Body.String(), "csrfToken") {
		t.Fatalf("token caller got a CSRF token: %s", rec.Body.String())
	}
}
