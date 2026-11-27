package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBindTokenBindsOnceWithinExpiry(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	kid, _ := s.CreateMember(ctx, child("Noa"))
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	if err := s.CreateBindToken(ctx, kid.ID, "tok-one", now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if id, err := s.ConsumeBindToken(ctx, "tok-one", 555, now); err != nil || id != kid.ID {
		t.Fatalf("first use: id=%d err=%v", id, err)
	}
	got, _ := s.GetMember(ctx, kid.ID)
	if got.TelegramChatID != 555 {
		t.Fatalf("chat id = %d, want 555", got.TelegramChatID)
	}
	if _, err := s.ConsumeBindToken(ctx, "tok-one", 666, now); !errors.Is(err, ErrBindInvalid) {
		t.Fatalf("reuse err = %v, want ErrBindInvalid", err)
	}
	got, _ = s.GetMember(ctx, kid.ID)
	if got.TelegramChatID != 555 {
		t.Fatalf("reuse changed chat id to %d", got.TelegramChatID)
	}
}

func TestBindTokenExpiresAndUnknownFails(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	kid, _ := s.CreateMember(ctx, child("Noa"))
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	_ = s.CreateBindToken(ctx, kid.ID, "tok-old", now.Add(time.Hour))
	if _, err := s.ConsumeBindToken(ctx, "tok-old", 1, now.Add(time.Hour)); !errors.Is(err, ErrBindInvalid) {
		t.Fatalf("expired token err = %v", err)
	}
	if _, err := s.ConsumeBindToken(ctx, "never-issued", 1, now); !errors.Is(err, ErrBindInvalid) {
		t.Fatalf("unknown token err = %v", err)
	}
}

func TestNewBindTokenReplacesOlderOne(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	kid, _ := s.CreateMember(ctx, child("Noa"))
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	_ = s.CreateBindToken(ctx, kid.ID, "first", now.Add(time.Hour))
	_ = s.CreateBindToken(ctx, kid.ID, "second", now.Add(time.Hour))
	if _, err := s.ConsumeBindToken(ctx, "first", 1, now); !errors.Is(err, ErrBindInvalid) {
		t.Fatalf("superseded token err = %v", err)
	}
	if _, err := s.ConsumeBindToken(ctx, "second", 1, now); err != nil {
		t.Fatalf("latest token: %v", err)
	}
}

func TestBindTokenIsStoredHashedOnly(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	kid, _ := s.CreateMember(ctx, child("Noa"))
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	_ = s.CreateBindToken(ctx, kid.ID, "plain-secret-token", now.Add(time.Hour))

	var stored string
	if err := s.db.QueryRowContext(ctx, `SELECT token_hash FROM tg_link_tokens`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "plain-secret-token") || stored != HashBindToken("plain-secret-token") {
		t.Fatalf("token not stored as its hash: %q", stored)
	}
}
