package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/auth/authtest"
	"github.com/moosequest/console/internal/core"

	_ "modernc.org/sqlite"
)

func TestKeyStoreContractMemory(t *testing.T) {
	authtest.RunKeyStoreContract(t, func(t *testing.T) auth.KeyStore { return newStore(t) })
}

func TestKeyStoreContractFile(t *testing.T) {
	authtest.RunKeyStoreContract(t, func(t *testing.T) auth.KeyStore {
		s, err := Open(context.Background(), filepath.Join(t.TempDir(), "c.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}

var kt0 = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

func testKey(id, name, tok string) auth.Key {
	return auth.Key{ID: id, Name: name, Digest: sha256.Sum256([]byte(tok)), Scope: auth.Admin, CreatedAt: kt0}
}

// TestKeysPersistAcrossReopen covers a fresh database, then reopening the
// same file (the schema re-runs and must be idempotent and lossless).
func TestKeysPersistAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateKey(ctx, testKey("k1", "ci", "secret-token")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAudit(ctx, auth.Event{ID: "e1", At: kt0, Principal: "root", Action: auth.ActionKeyCreate, Target: "k1", RequestID: "r1"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	for i := 0; i < 2; i++ {
		s, err = Open(ctx, path)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		k, err := s.LookupKey(ctx, sha256.Sum256([]byte("secret-token")))
		if err != nil || k.ID != "k1" {
			t.Fatalf("reopen %d lookup: %+v %v", i, k, err)
		}
		ev, _ := s.ListAudit(ctx, auth.AuditFilter{})
		if len(ev) != 1 || ev[0].RequestID != "r1" {
			t.Fatalf("reopen %d audit: %+v", i, ev)
		}
		s.Close()
	}
}

// legacySchema is the pre-change schema: flags/components/checks only.
const legacySchema = `
CREATE TABLE IF NOT EXISTS flags (key TEXT PRIMARY KEY, description TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 0,
	scope TEXT NOT NULL DEFAULT '', rollout INTEGER NOT NULL DEFAULT 0, variants TEXT NOT NULL DEFAULT '[]',
	cohort TEXT NOT NULL DEFAULT '', experiment TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS components (key TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '', config TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS checks (id INTEGER PRIMARY KEY AUTOINCREMENT, component TEXT NOT NULL, state INTEGER NOT NULL,
	message TEXT NOT NULL DEFAULT '', latency INTEGER NOT NULL DEFAULT 0, checked_at TEXT NOT NULL,
	FOREIGN KEY (component) REFERENCES components(key) ON DELETE CASCADE);
`

// TestExistingDatabaseUpgrade opens a database created before the key tables
// existed: the new tables, index, and column appear, and old data survives.
func TestExistingDatabaseUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(legacySchema +
		`INSERT INTO flags (key, enabled, created_at, updated_at) VALUES ('old-flag', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer s.Close()
	if f, err := s.GetFlag(ctx, "old-flag"); err != nil || !f.Enabled {
		t.Fatalf("legacy flag lost: %+v %v", f, err)
	}
	if err := s.CreateKey(ctx, testKey("k1", "ci", "tok")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAudit(ctx, auth.Event{ID: "e", At: kt0, Principal: "p", Action: auth.ActionKeyCreate, RequestID: "r"}); err != nil {
		t.Fatal(err)
	}
	var tbl string
	if err := s.db.QueryRow(`SELECT tbl_name FROM sqlite_master WHERE type='index' AND name='audit_events_at'`).Scan(&tbl); err != nil || tbl != "audit_events" {
		t.Fatalf("audit_events_at index: %q %v", tbl, err)
	}
	rows, err := s.db.Query(`PRAGMA table_info(audit_events)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, name)
	}
	if !strings.Contains(strings.Join(cols, ","), "request_id") {
		t.Fatalf("audit_events columns: %v", cols)
	}
}

// TestRollback proves older binaries are unaffected: the pre-change schema
// statements run cleanly against a database that already has the new tables,
// and the flag data is still readable alongside them.
func TestRollback(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateFlag(ctx, core.Flag{Key: "f", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateKey(ctx, testKey("k1", "ci", "tok")); err != nil {
		t.Fatal(err)
	}
	s.Close()

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(legacySchema); err != nil {
		t.Fatalf("pre-change schema against new database: %v", err)
	}
	var n int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM flags WHERE key = 'f'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("flag data: %d %v", n, err)
	}
}

// TestTokenNeverStored searches the database file (and WAL) for the plaintext
// token after closing; only the digest may be present.
func TestTokenNeverStored(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	const token = "console-super-secret-token-0123456789"
	if err := s.CreateKey(ctx, testKey("k1", "ci", token)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAudit(ctx, auth.Event{ID: "e", At: kt0, Principal: "p", Action: auth.ActionKeyCreate, Target: "k1"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	digest := sha256.Sum256([]byte(token))
	foundDigest := false
	for _, f := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(f)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(token)) {
			t.Fatalf("plaintext token found in %s", f)
		}
		if bytes.Contains(b, digest[:]) {
			foundDigest = true
		}
	}
	if !foundDigest {
		t.Error("digest not found in the database files (test would be vacuous)")
	}
}

func TestInvalidScopeRejected(t *testing.T) {
	s := newStore(t)
	k := testKey("k", "n", "t")
	k.Scope = 0
	if err := s.CreateKey(context.Background(), k); err == nil {
		t.Fatal("zero scope must be rejected")
	}
}

func TestPruneUsesIndex(t *testing.T) {
	s := newStore(t)
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN DELETE FROM audit_events WHERE at < ?`, "x")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var a, b, c int
		var detail string
		if err := rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
	}
	if !strings.Contains(plan.String(), "audit_events_at") {
		t.Fatalf("prune does not use the at index: %s", plan.String())
	}
}
