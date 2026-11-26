package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAccountSecretsRoundTripAndStayEncryptedOnDisk(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	a, err := s.CreateAccount(ctx, Account{Name: "home", Semel: 100008, Year: 2027, Username: "parent", Password: "hunter2", DeviceUUID: "uuid-1"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAccount(ctx, a.ID)
	if err != nil || got.Password != "hunter2" {
		t.Fatalf("GetAccount password = %q, err %v", got.Password, err)
	}
	if err := s.UpdateDevicePass(ctx, a.ID, `{"token":"0123456789abcdefghij"}`); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetAccount(ctx, a.ID)
	if !strings.Contains(got.DevicePass, "0123456789") {
		t.Fatalf("devicePass not round-tripped: %q", got.DevicePass)
	}

	// The raw column must not contain the plaintext.
	var raw []byte
	if err := s.DB().QueryRow(`SELECT password_sealed FROM accounts WHERE id = ?`, a.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("password stored in plaintext")
	}
}

func TestSealedSecretCannotBeMovedBetweenAccounts(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	a, _ := s.CreateAccount(ctx, Account{Name: "a", Semel: 1, Year: 2027, Username: "u", Password: "pw", DeviceUUID: "x"})
	b, _ := s.CreateAccount(ctx, Account{Name: "b", Semel: 1, Year: 2027, Username: "u", DeviceUUID: "y"})
	// Copy a's sealed password into b's row; b must refuse to open it.
	if _, err := s.DB().Exec(`UPDATE accounts SET password_sealed = (SELECT password_sealed FROM accounts WHERE id = ?) WHERE id = ?`, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAccount(ctx, b.ID); err == nil {
		t.Fatal("secret moved to another account must not decrypt")
	}
}

func TestItemsUpsertKeepsFirstSeenAndSoftDeletes(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	acc, _ := s.CreateAccount(ctx, Account{Name: "a", Semel: 1, Year: 2027, Username: "u", DeviceUUID: "x"})
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	base := Item{AccountID: acc.ID, ChildGUID: "c1", Domain: "homework", Key: "42", ContentHash: "h1", Payload: `{"a":1}`}

	if err := s.PutItem(ctx, base, t0); err != nil {
		t.Fatal(err)
	}
	upd := base
	upd.ContentHash, upd.Payload = "h2", `{"a":2}`
	if err := s.PutItem(ctx, upd, t1); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListItems(ctx, acc.ID, "c1", "homework")
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v, err %v", items, err)
	}
	if items[0].ContentHash != "h2" || !items[0].FirstSeen.Equal(t0) || !items[0].LastSeen.Equal(t1) {
		t.Fatalf("upsert wrong: %+v", items[0])
	}
	if err := s.SoftDeleteItem(ctx, acc.ID, "c1", "homework", "42", t1); err != nil {
		t.Fatal(err)
	}
	items, _ = s.ListItems(ctx, acc.ID, "c1", "homework")
	if len(items) != 0 {
		t.Fatal("soft-deleted item still listed")
	}
}

func TestBaselineFlag(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	acc, _ := s.CreateAccount(ctx, Account{Name: "a", Semel: 1, Year: 2027, Username: "u", DeviceUUID: "x"})
	ok, _ := s.IsBaselined(ctx, acc.ID, "c1", "grades")
	if ok {
		t.Fatal("new domain must not be baselined")
	}
	if err := s.MarkSynced(ctx, acc.ID, "c1", "grades", time.Now()); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.IsBaselined(ctx, acc.ID, "c1", "grades"); !ok {
		t.Fatal("domain should be baselined after MarkSynced")
	}
}

func TestEventDedupe(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Now()
	e := Event{Category: "homework", Kind: "new", ChildGUID: "c1", OccurredAt: now, DetectedAt: now,
		Data: `{}`, Source: "diff", DedupeKey: "c1|homework|42|h1"}
	first, err := s.InsertEvent(ctx, e)
	if err != nil || !first {
		t.Fatalf("first insert = %v, %v", first, err)
	}
	second, _ := s.InsertEvent(ctx, e)
	if second {
		t.Fatal("same dedupe key must not insert twice")
	}
	events, err := s.ListEvents(ctx, now.Add(-time.Minute), 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %d, err %v", len(events), err)
	}
}
