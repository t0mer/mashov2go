package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdminHashIsSetOnce(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if h, err := s.AdminPasswordHash(ctx); err != nil || h != "" {
		t.Fatalf("fresh store hash = %q, %v", h, err)
	}
	if set, err := s.SetAdminPasswordHashIfUnset(ctx, "$2a$first"); err != nil || !set {
		t.Fatalf("first set: %v %v", set, err)
	}
	if set, err := s.SetAdminPasswordHashIfUnset(ctx, "$2a$second"); err != nil || set {
		t.Fatalf("second set must be refused: %v %v", set, err)
	}
	if h, _ := s.AdminPasswordHash(ctx); h != "$2a$first" {
		t.Fatalf("hash = %q", h)
	}
}

func TestUISessionExpires(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	if err := s.CreateUISession(ctx, "hash-a", "csrf-a", now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	csrf, ok, err := s.UISession(ctx, "hash-a", now.Add(30*time.Minute))
	if err != nil || !ok || csrf != "csrf-a" {
		t.Fatalf("live session: csrf=%q ok=%v err=%v", csrf, ok, err)
	}
	if _, ok, _ := s.UISession(ctx, "hash-a", now.Add(time.Hour)); ok {
		t.Fatal("expired session accepted")
	}
	if _, ok, _ := s.UISession(ctx, "unknown", now); ok {
		t.Fatal("unknown session accepted")
	}
	if err := s.DeleteUISession(ctx, "hash-a"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.UISession(ctx, "hash-a", now); ok {
		t.Fatal("logged-out session still accepted")
	}
}

func TestAPITokenLifecycle(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	tok, err := s.CreateAPIToken(ctx, "dashboard", "read", "hash-read", now)
	if err != nil || tok.ID == 0 || tok.Scope != "read" {
		t.Fatalf("create: %+v %v", tok, err)
	}
	got, ok, err := s.ActiveAPIToken(ctx, "hash-read", now.Add(time.Minute))
	if err != nil || !ok || got.ID != tok.ID || got.Scope != "read" {
		t.Fatalf("active: %+v ok=%v err=%v", got, ok, err)
	}
	list, _ := s.ListAPITokens(ctx)
	if len(list) != 1 || list[0].LastUsedAt == nil {
		t.Fatalf("list = %+v", list)
	}

	if err := s.RevokeAPIToken(ctx, tok.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ActiveAPIToken(ctx, "hash-read", now.Add(3*time.Minute)); ok {
		t.Fatal("revoked token accepted")
	}
	if err := s.RevokeAPIToken(ctx, tok.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revoke err = %v, want ErrNotFound", err)
	}
	list, _ = s.ListAPITokens(ctx)
	if len(list) != 1 || list[0].RevokedAt == nil {
		t.Fatalf("revoked token should stay listed: %+v", list)
	}
}

func TestAPITokenScopeIsRestricted(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if _, err := s.CreateAPIToken(ctx, "x", "owner", "h", time.Now()); err == nil {
		t.Fatal("unknown scope accepted")
	}
}

func TestPasswordChangeEndsOtherSessions(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	_, _ = s.SetAdminPasswordHashIfUnset(ctx, "$2a$old")
	_ = s.CreateUISession(ctx, "keep", "c1", now.Add(time.Hour), now)
	_ = s.CreateUISession(ctx, "other", "c2", now.Add(time.Hour), now)

	if err := s.SetAdminPasswordHash(ctx, "$2a$new"); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.AdminPasswordHash(ctx); h != "$2a$new" {
		t.Fatalf("hash = %q", h)
	}
	if err := s.DeleteUISessionsExcept(ctx, "keep"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.UISession(ctx, "other", now); ok {
		t.Fatal("other session survived the password change")
	}
	if _, ok, _ := s.UISession(ctx, "keep", now); !ok {
		t.Fatal("current session was ended")
	}
}
