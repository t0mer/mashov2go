package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConnectorConfigSealedAndNotListed(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, filepath.Join(dir, "test.db"), testSealer(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	secret := []byte(`{"instance_id":"1101","token":"SECRET-TOKEN-VALUE-123456"}`)
	c, err := s.CreateConnector(ctx, Connector{Type: "greenapi", Name: "main", IsDefault: true, Enabled: true}, secret)
	if err != nil || c.ID == 0 {
		t.Fatalf("create: %+v %v", c, err)
	}
	got, err := s.ConnectorConfig(ctx, c.ID)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("round trip: %q %v", got, err)
	}
	list, err := s.ListConnectors(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "main" || !list[0].IsDefault {
		t.Fatalf("list: %+v %v", list, err)
	}
	// The plaintext token must not appear anywhere in the database file.
	_, _ = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	for _, name := range []string{"test.db", "test.db-wal"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil && bytes.Contains(raw, []byte("SECRET-TOKEN-VALUE")) {
			t.Fatalf("connector token stored in plaintext in %s", name)
		}
	}
}

func TestConnectorSealBoundToRow(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	a, _ := s.CreateConnector(ctx, Connector{Type: "telegram", Name: "a", Enabled: true}, []byte(`{"token":"A"}`))
	b, _ := s.CreateConnector(ctx, Connector{Type: "telegram", Name: "b", Enabled: true}, []byte(`{"token":"B"}`))
	// Swap the sealed blobs: decryption must fail because the AAD names the row.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE connectors SET config_sealed = (SELECT config_sealed FROM connectors WHERE id = ?) WHERE id = ?`, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConnectorConfig(ctx, b.ID); err == nil {
		t.Fatal("sealed config moved between connectors was accepted")
	}
}

func TestConnectorDefaultAndTelegramRules(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	w1, _ := s.CreateConnector(ctx, Connector{Type: "greenapi", Name: "w1", IsDefault: true, Enabled: true}, []byte(`{}`))
	w2, _ := s.CreateConnector(ctx, Connector{Type: "greenapi", Name: "w2", Enabled: true}, []byte(`{}`))
	if err := s.SetDefaultConnector(ctx, w2.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListConnectors(ctx)
	for _, c := range list {
		if (c.ID == w2.ID) != c.IsDefault {
			t.Errorf("default flag wrong on %s", c.Name)
		}
	}
	_ = w1
	tg, _ := s.CreateConnector(ctx, Connector{Type: "telegram", Name: "bot", Enabled: true}, []byte(`{}`))
	if err := s.SetDefaultConnector(ctx, tg.ID); err == nil {
		t.Error("telegram connector became default")
	}
	if _, err := s.CreateConnector(ctx, Connector{Type: "telegram", Name: "bot2", IsDefault: true}, []byte(`{}`)); err == nil {
		t.Error("telegram created as default")
	}
}

func TestConnectorUpdateKeepsSecretWhenNil(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	c, _ := s.CreateConnector(ctx, Connector{Type: "greenapi", Name: "x", Enabled: true}, []byte(`{"token":"keep"}`))
	if err := s.UpdateConnector(ctx, c.ID, "renamed", false, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ConnectorConfig(ctx, c.ID)
	if string(got) != `{"token":"keep"}` {
		t.Errorf("secret changed: %q", got)
	}
	if err := s.UpdateConnector(ctx, c.ID, "renamed", false, []byte(`{"token":"new"}`)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ConnectorConfig(ctx, c.ID)
	if string(got) != `{"token":"new"}` {
		t.Errorf("secret not replaced: %q", got)
	}
}

func TestDeleteConnectorBlockedWhileInUse(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	c, _ := s.CreateConnector(ctx, Connector{Type: "greenapi", Name: "x", Enabled: true}, []byte(`{}`))
	m := child("Noa")
	m.WhatsAppConnectorID = c.ID
	if _, err := s.CreateMember(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteConnector(ctx, c.ID); !errors.Is(err, ErrConnectorInUse) {
		t.Fatalf("expected ErrConnectorInUse, got %v", err)
	}
}
