package mashov

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient points a Client at srv with no pacing and near-zero backoff.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(Options{BaseURL: srv.URL + "/api/", Backoff: time.Millisecond})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.minGap = 0 // zero in Options means "default"; tests disable pacing explicitly
	return c
}

const loginOKBody = `{
  "credential": {"semel": 100005, "year": 2027, "userId": 42, "userType": 1, "idNumber": "123"},
  "accessToken": {
    "displayName": "Parent Name",
    "children": [{"childGuid": "guid-1", "privateName": "Noa", "familyName": "Klein", "classCode": 3, "classNum": 2}],
    "externChildren": [],
    "schoolOptions": {"hasParentsBehave": true}
  }
}`

func TestLoginSuccessStoresCSRFAndEchoesIt(t *testing.T) {
	var gotCSRF atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("login method = %s", r.Method)
		}
		w.Header().Set("X-Csrf-Token", "csrf-abc")
		w.Header().Set("DevicePass", `{"token":"0123456789abcdefghij"}`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(loginOKBody))
	})
	mux.HandleFunc("/api/students/guid-1/homework", func(w http.ResponseWriter, r *http.Request) {
		gotCSRF.Store(r.Header.Get("X-Csrf-Token"))
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	res, err := c.Login(context.Background(), LoginRequest{Semel: 100005, Year: 2027, Username: "u", Password: "p"}, "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if len(res.AccessToken.Children) != 1 || res.AccessToken.Children[0].ChildGUID != "guid-1" {
		t.Fatalf("children not decoded: %+v", res.AccessToken.Children)
	}
	if res.DevicePass == "" {
		t.Fatal("devicePass >= 20 chars should be captured")
	}
	if c.CSRF() != "csrf-abc" {
		t.Fatalf("CSRF = %q", c.CSRF())
	}
	if _, err := c.Get(context.Background(), "students/guid-1/homework", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, _ := gotCSRF.Load().(string); v != "csrf-abc" {
		t.Fatalf("X-Csrf-Token not echoed, got %q", v)
	}
}

func TestLoginFailureReasons(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reason string
		want   State
	}{
		{"bad credentials no reason", 400, "", StateAuthFailed},
		{"user suspended", 400, "UserSuspended", StateNeedsManual},
		{"change pass", 400, "ChangePass", StateNeedsManual},
		{"create username", 400, "CreateUsername", StateNeedsManual},
		{"set username and pass", 400, "SetUsernameAndPass", StateNeedsManual},
		{"captcha required", 400, "CaptchaRequired", StateNeedsManual},
		{"no parents access", 400, "NoParentsAccess", StateBlocked},
		{"school closed", 400, "SchoolClosed", StateAuthFailed},
		{"inactive in year", 400, "UserInactiveInYear", StateAuthFailed},
		{"unknown reason", 400, "CellphoneLocked", StateAuthFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.reason != "" {
					w.Header().Set("Reason", tc.reason)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			c := newTestClient(t, srv)
			_, err := c.Login(context.Background(), LoginRequest{Semel: 1, Year: 2027}, "")
			var le *LoginError
			if !errors.As(err, &le) {
				t.Fatalf("want *LoginError, got %v", err)
			}
			if le.State != tc.want || le.Reason != tc.reason {
				t.Fatalf("got state=%s reason=%q, want state=%s", le.State, le.Reason, tc.want)
			}
		})
	}
}

func TestFailedLoginIsNeverRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.Login(context.Background(), LoginRequest{}, ""); err == nil {
		t.Fatal("expected error")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("login sent %d requests, want exactly 1 (no automatic retry)", n)
	}
}

func TestSessionExpiryReauthsOnceAndReplays(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(419)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	reauths := 0
	c.SetReauth(func(ctx context.Context) error { reauths++; return nil })

	if _, err := c.Get(context.Background(), "timetable", nil); err != nil {
		t.Fatalf("Get after reauth: %v", err)
	}
	if reauths != 1 || calls.Load() != 2 {
		t.Fatalf("reauths=%d requests=%d, want 1 and 2", reauths, calls.Load())
	}
}

func TestSessionExpiryGivesUpAfterSecondFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	reauths := 0
	c.SetReauth(func(ctx context.Context) error { reauths++; return nil })
	_, err := c.Get(context.Background(), "timetable", nil)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	if reauths != 1 {
		t.Fatalf("reauths = %d, want 1", reauths)
	}
}

func TestGetRetriesOn429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.Get(context.Background(), "bells", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestGetGivesUpWithRateLimitedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.Get(context.Background(), "bells", nil); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestRequestsAreSpacedByMinGap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	const gap = 60 * time.Millisecond
	c, err := NewClient(Options{BaseURL: srv.URL + "/api/", MinGap: gap})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), "bells", nil); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 2*gap {
		t.Fatalf("3 requests took %v, want at least %v of pacing", elapsed, 2*gap)
	}
}

func TestSchoolsDecodesAndPicksNewestYear(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"semel":100008,"name":"X","years":[2025,2027,2026]}]`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	schools, err := c.Schools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(schools) != 1 || schools[0].DefaultYear() != 2027 {
		t.Fatalf("schools = %+v", schools)
	}
}

func TestNewClientRejectsPlainHTTPForRemoteHost(t *testing.T) {
	if _, err := NewClient(Options{BaseURL: "http://example.com/api/"}); err == nil {
		t.Fatal("expected error for non-https remote base url")
	}
}

func TestRedactJSONRemovesPII(t *testing.T) {
	out := string(RedactJSON([]byte(loginOKBody)))
	for _, secret := range []string{"Parent Name", "guid-1", "Noa", "Klein", `"123"`} {
		if strings.Contains(out, secret) {
			t.Fatalf("redacted output still contains %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "hasParentsBehave") {
		t.Fatal("non-PII structure should survive redaction")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("redacted output is not valid JSON: %v", err)
	}
}

func TestRedactJSONHidesNonJSON(t *testing.T) {
	if out := string(RedactJSON([]byte("<html>Noa</html>"))); strings.Contains(out, "Noa") {
		t.Fatalf("non-JSON body leaked: %s", out)
	}
}

func TestRedirectOffOriginIsRefused(t *testing.T) {
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHits.Add(1)
	}))
	defer foreign.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/steal", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.SetCSRFForTest("csrf-secret")
	if _, err := c.Get(context.Background(), "timetable", nil); err == nil {
		t.Fatal("expected redirect to foreign host to be refused")
	}
	if foreignHits.Load() != 0 {
		t.Fatal("foreign host received a request")
	}
}

func TestIsActiveDoesNotTriggerReauth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	reauths := 0
	c.SetReauth(func(ctx context.Context) error { reauths++; return nil })
	if err := c.IsActive(context.Background()); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	if reauths != 0 {
		t.Fatalf("IsActive triggered %d re-auths, want 0", reauths)
	}
}

func TestGetSplitsQueryWrittenIntoPath(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c, err := NewClient(Options{BaseURL: srv.URL + "/api/", MinGap: time.Microsecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), "students/sg-1/grades?start=2026-09-01T00:00:00Z&take=5", url.Values{"skip": {"0"}}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/students/sg-1/grades" {
		t.Fatalf("path = %q", gotPath)
	}
	q, _ := url.ParseQuery(gotQuery)
	if q.Get("start") != "2026-09-01T00:00:00Z" || q.Get("take") != "5" || q.Get("skip") != "0" {
		t.Fatalf("query = %q", gotQuery)
	}
}
