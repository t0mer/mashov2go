package store

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/t0mer/mashov2go/internal/crypto"
)

// TestMigration004KeepsConnectorsAndMembers builds the schema as it was before
// 004, stores a connector and a member that uses it, then applies 004.
func TestMigration004KeepsConnectorsAndMembers(t *testing.T) {
	ctx := context.Background()
	sealer, err := crypto.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "old.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	s := &Store{db: db, sealer: sealer}

	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	apply := func(name string) {
		t.Helper()
		body, err := fs.ReadFile(migrationFS, "migrations/"+name)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.apply(ctx, name, string(body)); err != nil {
			t.Fatalf("applying %s: %v", name, err)
		}
	}
	apply("001_init.sql")
	apply("002_family.sql")
	apply("003_telegram_bind.sql")

	sealed := []byte("sealed-config-bytes")
	res, err := db.ExecContext(ctx,
		`INSERT INTO connectors(type, name, config_sealed, is_default, enabled) VALUES('greenapi','home',?,1,1)`, sealed)
	if err != nil {
		t.Fatal(err)
	}
	connID, _ := res.LastInsertId()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO members(role, name, created_at, whatsapp_connector_id) VALUES('child','Noa','2026-11-27T00:00:00Z',?)`, connID); err != nil {
		t.Fatal(err)
	}

	apply("004_openwa.sql")

	var got []byte
	var name string
	if err := db.QueryRowContext(ctx, `SELECT config_sealed, name FROM connectors WHERE id = ?`, connID).Scan(&got, &name); err != nil {
		t.Fatalf("connector lost: %v", err)
	}
	if !bytes.Equal(got, sealed) || name != "home" {
		t.Fatalf("connector changed: name=%q sealed=%q", name, got)
	}
	var ref sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT whatsapp_connector_id FROM members WHERE name = 'Noa'`).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	if !ref.Valid || ref.Int64 != connID {
		t.Fatalf("member reference lost: %+v", ref)
	}
	var violations int
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		violations++
	}
	_ = rows.Close()
	if violations != 0 {
		t.Fatalf("foreign key violations: %d", violations)
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO connectors(type, name, config_sealed) VALUES('openwa','gw',?)`, sealed); err != nil {
		t.Fatalf("openwa type rejected: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO connectors(type, name, config_sealed) VALUES('bogus','x',?)`, sealed); err == nil {
		t.Fatal("unknown connector type accepted")
	}
}
