package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashAndCheck(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse battery") {
		t.Fatal("right password rejected")
	}
	if CheckPassword(h, "correct horse batterX") {
		t.Fatal("wrong password accepted")
	}
	if CheckPassword("", "correct horse battery") {
		t.Fatal("empty hash accepted")
	}
	if strings.Contains(h, "correct") {
		t.Fatal("hash contains the password")
	}
}

func TestPasswordLengthBounds(t *testing.T) {
	if _, err := HashPassword("short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("short password: %v", err)
	}
	if _, err := HashPassword(strings.Repeat("x", 73)); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("over bcrypt limit: %v", err)
	}
	if _, err := HashPassword(strings.Repeat("x", 12)); err != nil {
		t.Fatalf("12 bytes rejected: %v", err)
	}
}

func TestTokensAreRandomAndHashed(t *testing.T) {
	a, _ := NewToken()
	b, _ := NewToken()
	if a == b || len(a) < 40 {
		t.Fatalf("tokens not random enough: %q %q", a, b)
	}
	if HashToken(a) == a || HashToken(a) != HashToken(a) || HashToken(a) == HashToken(b) {
		t.Fatal("hash is not a stable one-way form of the token")
	}
}

func TestLimiterBlocksAfterMaxFailuresUntilWindowEnds(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	l := &Limiter{Max: 3, Window: 15 * time.Minute, Now: func() time.Time { return now }}

	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("blocked too early at attempt %d", i)
		}
		l.Failed("1.2.3.4")
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("still allowed after max failures")
	}
	if !l.Allow("5.6.7.8") {
		t.Fatal("other clients must not be blocked")
	}
	now = now.Add(15 * time.Minute)
	if !l.Allow("1.2.3.4") {
		t.Fatal("not unblocked after the window")
	}
}

func TestLimiterSuccessClearsFailures(t *testing.T) {
	l := &Limiter{Max: 2, Window: time.Hour}
	l.Failed("k")
	l.Failed("k")
	l.Succeeded("k")
	if !l.Allow("k") {
		t.Fatal("success did not clear failures")
	}
}
