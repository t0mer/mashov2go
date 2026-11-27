// Package api serves the REST API under /api/v1 (CLAUDE.md §9). Browser clients
// use a session cookie with a CSRF token; scripts use Bearer API tokens. Errors
// are RFC 7807 problem documents.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/t0mer/mashov2go/internal/auth"
	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/store"
)

const (
	cookieName   = "m2g_session"
	sessionTTL   = 12 * time.Hour
	maxBodyBytes = 1 << 20
)

// Server is the REST API. Construct it with a Store and call Handler.
type Server struct {
	Store *store.Store
	// Limiter throttles failed logins. Nil uses 5 failures per 15 minutes.
	Limiter *auth.Limiter
	// SecureCookies sets the Secure attribute; enable it behind HTTPS.
	SecureCookies bool
	// TrustedProxies are the addresses whose X-Forwarded-For is honoured for
	// rate limiting (for example the local Cloudflare Tunnel connector).
	TrustedProxies []netip.Prefix
	Now            func() time.Time
	// MashovBaseURL overrides mashov.DefaultBaseURL; MashovTransport is for tests.
	MashovBaseURL   string
	MashovTransport http.RoundTripper
}

// Handler returns the routed API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/v1/setup", s.setupStatus)
	mux.HandleFunc("POST /api/v1/setup", s.setup)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.Handle("POST /api/v1/auth/logout", s.authed("admin", http.HandlerFunc(s.logout)))
	mux.Handle("GET /api/v1/auth/me", s.authed("read", http.HandlerFunc(s.me)))
	mux.Handle("POST /api/v1/settings/admin-password", s.authed("admin", http.HandlerFunc(s.changePassword)))
	mux.Handle("GET /api/v1/tokens", s.authed("admin", http.HandlerFunc(s.listTokens)))
	mux.Handle("POST /api/v1/tokens", s.authed("admin", http.HandlerFunc(s.createToken)))
	mux.Handle("DELETE /api/v1/tokens/{id}", s.authed("admin", http.HandlerFunc(s.revokeToken)))
	s.accountRoutes(mux)
	s.familyRoutes(mux, "/api/v1/children", family.RoleChild)
	s.familyRoutes(mux, "/api/v1/parents", family.RoleParent)
	return mux
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) limiter() *auth.Limiter {
	if s.Limiter == nil {
		s.Limiter = &auth.Limiter{Max: 5, Window: 15 * time.Minute}
	}
	return s.Limiter
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// clientIP is the address rate limiting keys on. X-Forwarded-For is read only
// when the direct peer is a trusted proxy. Each proxy appends the address it
// received from, so the chain is walked from the right and the first address
// that is not a trusted proxy is the client. Entries to the left of that are
// client-supplied and ignored.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	if !s.trusted(peer) {
		return peer.String()
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			break
		}
		if !s.trusted(a) {
			return a.String()
		}
	}
	return peer.String()
}

func (s *Server) trusted(a netip.Addr) bool {
	for _, p := range s.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// problem writes an RFC 7807 problem document.
func problem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  title,
		"status": status,
		"detail": detail,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decode reads a bounded JSON body into v. Unknown fields are rejected.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			problem(w, http.StatusBadRequest, "Bad request", "a JSON body is required")
		} else {
			problem(w, http.StatusBadRequest, "Bad request", "the JSON body is not valid")
		}
		return false
	}
	return true
}

func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
}
