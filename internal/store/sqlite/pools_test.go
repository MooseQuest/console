package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/moosequest/console/internal/core"
)

// TestUpgradeRollbackJournalDB opens a database created in the legacy
// rollback-journal mode and checks it converts to WAL with data intact.
func TestUpgradeRollbackJournalDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(schema); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := raw.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "delete" {
		t.Fatalf("legacy mode = %q (%v), want delete", mode, err)
	}
	if err := func() error {
		s := &Store{db: raw, rdb: raw}
		return s.CreateFlag(context.Background(), core.Flag{Key: "old"})
	}(); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.rdb.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("upgraded mode = %q (%v), want wal", mode, err)
	}
	if _, err := s.GetFlag(context.Background(), "old"); err != nil {
		t.Errorf("legacy row not readable: %v", err)
	}
}

// TestPathWithSpaces covers file paths containing spaces.
func TestPathWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my data dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), filepath.Join(dir, "c o n.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateFlag(context.Background(), core.Flag{Key: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetFlag(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
}

func TestReaderRejectsWritesAndDoubleClose(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.rdb.Exec(`DELETE FROM flags`); err == nil {
		t.Error("reader pool accepted a write")
	}
	if err := s.Close(); err != nil {
		t.Error(err)
	}
	_ = s.Close() // double close must not panic
}
