package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moosequest/console/internal/auth"
)

// TestFileDBWALAndPragmas pins the connection setup that prevents
// SQLITE_BUSY: WAL journaling and a busy timeout on the file-backed store.
func TestFileDBWALAndPragmas(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for name, db := range map[string]*sql.DB{"writer": s.db, "reader": s.rdb} {
		var mode string
		if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
			t.Errorf("%s journal_mode = %q (%v), want wal", name, mode, err)
		}
		var ms int
		if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&ms); err != nil || ms != 5000 {
			t.Errorf("%s busy_timeout = %d (%v), want 5000", name, ms, err)
		}
	}
}

// TestFileDBConcurrentWritersAcrossHandles hammers one file from two Store
// handles (standing in for the CLI and the server sharing a database) with
// many goroutines; no operation may fail with SQLITE_BUSY.
func TestFileDBConcurrentWritersAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.db")
	var stores []*Store
	for i := 0; i < 2; i++ {
		s, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		stores = append(stores, s)
	}
	ctx := context.Background()
	if err := stores[0].CreateKey(ctx, testKey("k", "n", "t")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for w := 0; w < 32; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := stores[w%2]
			for j := 0; j < 40; j++ {
				if err := s.TouchKey(ctx, "k", kt0.Add(time.Duration(j)*time.Second)); err != nil {
					t.Errorf("touch: %v", err)
				}
				e := auth.Event{ID: fmt.Sprintf("e-%d-%d", w, j), At: kt0, Principal: "p", Action: auth.ActionKeyCreate, Target: "k"}
				if err := s.AppendAudit(ctx, e); err != nil {
					t.Errorf("append: %v", err)
				}
				if _, err := s.ListAudit(ctx, auth.AuditFilter{Limit: 5}); err != nil {
					t.Errorf("list: %v", err)
				}
				if _, err := s.PruneAudit(ctx, kt0.Add(-time.Hour)); err != nil {
					t.Errorf("prune: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}
