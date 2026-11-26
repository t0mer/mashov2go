package store

import (
	"bytes"
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/t0mer/mashov2go/internal/crypto"
)

func testSealer(t *testing.T) *crypto.Sealer {
	t.Helper()
	s, err := crypto.NewSealer(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), testSealer(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrationsApplyOnceAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	s, err := Open(context.Background(), path, testSealer(t))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	want := len(embeddedMigrations(t))
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != want {
		t.Fatalf("applied migrations = %d, want %d, err %v", n, want, err)
	}
	_ = s.Close()
	s2, err := Open(context.Background(), path, testSealer(t))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if err := s2.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != want {
		t.Fatalf("after reopen migrations = %d, want %d, err %v", n, want, err)
	}
}

func embeddedMigrations(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestSplitStatementsDropsComments(t *testing.T) {
	got := splitStatements("-- header\nCREATE TABLE a (x INT); -- trailing\nCREATE TABLE b (y INT);")
	if len(got) != 2 {
		t.Fatalf("statements = %d: %q", len(got), got)
	}
}
