// Package auth holds the credential primitives for the admin UI and the REST
// API (CLAUDE.md §9, §12): bcrypt admin passwords, random session and API
// tokens stored only as SHA-256 hashes, and a login rate limiter.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLen is the shortest admin password accepted.
const MinPasswordLen = 12

// Cost is the bcrypt work factor for new hashes. Tests may lower it; production
// keeps bcrypt.DefaultCost.
var Cost = bcrypt.DefaultCost

// maxPasswordBytes is bcrypt's input limit; longer passwords would be silently truncated.
const maxPasswordBytes = 72

// ErrWeakPassword is returned for a password outside the accepted length.
var ErrWeakPassword = errors.New("password must be 12 to 72 bytes")

// HashPassword returns a bcrypt hash of password.
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLen || len(password) > maxPasswordBytes {
		return "", ErrWeakPassword
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), Cost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword reports whether password matches hash. An empty hash never matches.
func CheckPassword(hash, password string) bool {
	if hash == "" || len(password) > maxPasswordBytes {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewToken returns a random 256-bit token, URL-safe, for a session or API token.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken is the form a token is stored in.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Limiter throttles failed logins per key (the client address). After Max
// failures within Window the key is blocked until the window ends.
type Limiter struct {
	Max    int
	Window time.Duration
	Now    func() time.Time

	mu    sync.Mutex
	state map[string]limit
}

type limit struct {
	count int
	first time.Time
}

// Allow reports whether key may attempt a login now.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.state[key]
	if !ok {
		return true
	}
	if l.now().Sub(st.first) >= l.Window {
		delete(l.state, key)
		return true
	}
	return st.count < l.Max
}

// Failed records a failed attempt for key.
func (l *Limiter) Failed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state == nil {
		l.state = map[string]limit{}
	}
	now := l.now()
	st, ok := l.state[key]
	if !ok || now.Sub(st.first) >= l.Window {
		st = limit{first: now}
	}
	st.count++
	l.state[key] = st
}

// Succeeded clears the failures for key after a good login.
func (l *Limiter) Succeeded(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.state, key)
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// NewUUID returns a random RFC 4122 version 4 UUID, used as the per-account
// device identity sent to Mashov (CLAUDE.md §3.2).
func NewUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}
