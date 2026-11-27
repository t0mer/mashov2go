package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/t0mer/mashov2go/internal/auth"
	"github.com/t0mer/mashov2go/internal/store"
)

// setupStatus tells the UI whether the admin password has been set (first run).
func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	h, err := s.Store.AdminPasswordHash(r.Context())
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "reading setup state failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": h != ""})
}

// setup sets the admin password on first run and signs the caller in. It is
// refused once a password exists.
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", "the password must be 12 to 72 bytes")
		return
	}
	set, err := s.Store.SetAdminPasswordHashIfUnset(r.Context(), hash)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "saving the password failed")
		return
	}
	if !set {
		problem(w, http.StatusConflict, "Already set up", "the admin password is already set")
		return
	}
	s.startSession(w, r, http.StatusCreated)
}

// login checks the admin password and starts a session. Failed attempts are
// counted per client address and refused for the rest of the window.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	key := s.clientIP(r)
	if !s.limiter().Allow(key) {
		problem(w, http.StatusTooManyRequests, "Too many attempts", "wait before trying again")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	hash, err := s.Store.AdminPasswordHash(r.Context())
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "reading the password failed")
		return
	}
	if hash == "" {
		problem(w, http.StatusConflict, "Not set up", "set the admin password first")
		return
	}
	if !auth.CheckPassword(hash, in.Password) {
		s.limiter().Failed(key)
		problem(w, http.StatusUnauthorized, "Invalid credentials", "the password is not correct")
		return
	}
	s.limiter().Succeeded(key)
	s.startSession(w, r, http.StatusOK)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, status int) {
	token, err := auth.NewToken()
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "creating a session failed")
		return
	}
	csrf, err := auth.NewToken()
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "creating a session failed")
		return
	}
	now := s.now()
	if err := s.Store.CreateUISession(r.Context(), auth.HashToken(token), csrf, now.Add(sessionTTL), now); err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "creating a session failed")
		return
	}
	setSessionCookie(w, token, s.SecureCookies)
	writeJSON(w, status, map[string]string{"csrfToken": csrf})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	p := fromContext(r.Context())
	if p.kind == "session" {
		if err := s.Store.DeleteUISession(r.Context(), p.hash); err != nil {
			problem(w, http.StatusInternalServerError, "Internal error", "ending the session failed")
			return
		}
	}
	clearSessionCookie(w, s.SecureCookies)
	w.WriteHeader(http.StatusNoContent)
}

// me describes the caller. A session caller also gets its CSRF token, so a page
// reload can resume without signing in again.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := fromContext(r.Context())
	out := map[string]string{"kind": p.kind, "scope": p.scope}
	if p.kind == "session" {
		out["csrfToken"] = p.csrf
	}
	writeJSON(w, http.StatusOK, out)
}

// changePassword replaces the admin password and ends every other session.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &in) {
		return
	}
	hash, err := s.Store.AdminPasswordHash(r.Context())
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "reading the password failed")
		return
	}
	if !auth.CheckPassword(hash, in.Current) {
		problem(w, http.StatusUnauthorized, "Invalid credentials", "the current password is not correct")
		return
	}
	newHash, err := auth.HashPassword(in.New)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", "the new password must be 12 to 72 bytes")
		return
	}
	if err := s.Store.SetAdminPasswordHash(r.Context(), newHash); err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "saving the password failed")
		return
	}
	if err := s.Store.DeleteUISessionsExcept(r.Context(), fromContext(r.Context()).hash); err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "ending other sessions failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tokenView struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Scope      string  `json:"scope"`
	CreatedAt  string  `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt"`
	RevokedAt  *string `json:"revokedAt"`
}

func viewToken(t store.APIToken) tokenView {
	v := tokenView{ID: t.ID, Name: t.Name, Scope: t.Scope, CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z07:00")}
	if t.LastUsedAt != nil {
		s := t.LastUsedAt.Format("2006-01-02T15:04:05Z07:00")
		v.LastUsedAt = &s
	}
	if t.RevokedAt != nil {
		s := t.RevokedAt.Format("2006-01-02T15:04:05Z07:00")
		v.RevokedAt = &s
	}
	return v
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListAPITokens(r.Context())
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "listing tokens failed")
		return
	}
	out := make([]tokenView, 0, len(list))
	for _, t := range list {
		out = append(out, viewToken(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// createToken makes a new API token. The secret is returned once and never stored.
func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if !decode(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 64 {
		problem(w, http.StatusBadRequest, "Bad request", "name must be 1 to 64 characters")
		return
	}
	if in.Scope != "read" && in.Scope != "admin" {
		problem(w, http.StatusBadRequest, "Bad request", "scope must be read or admin")
		return
	}
	raw, err := auth.NewToken()
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "creating the token failed")
		return
	}
	t, err := s.Store.CreateAPIToken(r.Context(), name, in.Scope, auth.HashToken(raw), s.now())
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "saving the token failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": raw, "info": viewToken(t)})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		problem(w, http.StatusBadRequest, "Bad request", "the token id is not valid")
		return
	}
	if err := s.Store.RevokeAPIToken(r.Context(), id, s.now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			problem(w, http.StatusNotFound, "Not found", "no active token has that id")
			return
		}
		problem(w, http.StatusInternalServerError, "Internal error", "revoking the token failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
