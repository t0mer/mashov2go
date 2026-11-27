package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/t0mer/mashov2go/internal/auth"
)

type principalKey struct{}

// principal is the authenticated caller of one request.
type principal struct {
	kind  string // "session" or "token"
	scope string // "admin" for sessions; the token's scope otherwise
	csrf  string // session CSRF token; empty for tokens
	// hash is the session token hash, used to end this session on logout.
	hash string
}

func fromContext(ctx context.Context) principal {
	p, _ := ctx.Value(principalKey{}).(principal)
	return p
}

// authed requires a session or token with at least the given scope ("read" or
// "admin"). A read-scope token can only use safe methods. Session requests that
// change state must send the CSRF token in X-CSRF-Token.
func (s *Server) authed(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok, err := s.authenticate(r)
		if err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "authentication failed")
			return
		}
		if !ok {
			problem(w, http.StatusUnauthorized, "Unauthorized", "sign in or send a valid API token")
			return
		}
		if scope == "admin" && p.scope != "admin" {
			problem(w, http.StatusForbidden, "Forbidden", "this token cannot do that")
			return
		}
		if p.kind == "session" && !safeMethod(r.Method) {
			if p.csrf == "" || r.Header.Get("X-CSRF-Token") != p.csrf {
				problem(w, http.StatusForbidden, "Forbidden", "missing or wrong CSRF token")
				return
			}
		}
		if p.kind == "token" && p.scope == "read" && !safeMethod(r.Method) {
			problem(w, http.StatusForbidden, "Forbidden", "a read-only token cannot change data")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

func safeMethod(m string) bool { return m == http.MethodGet || m == http.MethodHead }

// authenticate resolves the caller from the session cookie or a Bearer token.
func (s *Server) authenticate(r *http.Request) (principal, bool, error) {
	now := s.now()
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		hash := auth.HashToken(c.Value)
		csrf, ok, err := s.Store.UISession(r.Context(), hash, now)
		if err != nil {
			return principal{}, false, err
		}
		if ok {
			return principal{kind: "session", scope: "admin", csrf: csrf, hash: hash}, true, nil
		}
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if raw == "" {
			return principal{}, false, nil
		}
		t, ok, err := s.Store.ActiveAPIToken(r.Context(), auth.HashToken(raw), now)
		if err != nil {
			return principal{}, false, err
		}
		if ok {
			return principal{kind: "token", scope: t.Scope}, true, nil
		}
	}
	return principal{}, false, nil
}
