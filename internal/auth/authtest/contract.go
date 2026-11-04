// Package authtest provides a reusable behavioural contract suite for
// auth.KeyStore implementations, so every backend (the in-memory reference and
// SQLite today, plugin stores later) is held to the same semantics. It is
// imported only from tests and is never linked into the Console binary.
package authtest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moosequest/console/internal/auth"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 123456789, time.UTC)

func digestOf(tok string) [sha256.Size]byte { return sha256.Sum256([]byte(tok)) }

func mkKey(id, name, tok string, sc auth.Scope) auth.Key {
	return auth.Key{ID: id, Name: name, Digest: digestOf(tok), Scope: sc, CreatedAt: t0}
}

// RunKeyStoreContract runs the KeyStore contract against stores returned by
// newStore. Each subtest gets a fresh, empty store.
func RunKeyStoreContract(t *testing.T, newStore func(t *testing.T) auth.KeyStore) {
	t.Helper()
	cases := []struct {
		name string
		fn   func(t *testing.T, ks auth.KeyStore)
	}{
		{"CreateLookupRoundTrip", testRoundTrip},
		{"DuplicateRejection", testDuplicates},
		{"ListKeys", testListKeys},
		{"Revoke", testRevoke},
		{"Touch", testTouch},
		{"Audit", testAudit},
		{"Prune", testPrune},
		{"ConcurrentCreateSameName", testConcurrentCreate},
		{"ConcurrentRevoke", testConcurrentRevoke},
		{"ConcurrentMixed", testConcurrentMixed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.fn(t, newStore(t)) })
	}
}

func testRoundTrip(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	if _, err := ks.LookupKey(ctx, digestOf("none")); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("lookup unknown: %v", err)
	}
	want := mkKey("k1", "alpha", "tok1", auth.Write)
	want.ExpiresAt = t0.Add(48 * time.Hour)
	if err := ks.CreateKey(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := ks.LookupKey(ctx, digestOf("tok1"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "k1" || got.Name != "alpha" || got.Scope != auth.Write || got.Digest != want.Digest {
		t.Fatalf("roundtrip: %+v", got)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("times: created %v expires %v", got.CreatedAt, got.ExpiresAt)
	}
	if !got.LastUsedAt.IsZero() || !got.RevokedAt.IsZero() {
		t.Fatalf("unset times must stay zero: %+v", got)
	}
	// A key without expiry stays non-expiring.
	if err := ks.CreateKey(ctx, mkKey("k2", "beta", "tok2", auth.Eval)); err != nil {
		t.Fatal(err)
	}
	if k, _ := ks.LookupKey(ctx, digestOf("tok2")); !k.ExpiresAt.IsZero() || k.Scope != auth.Eval {
		t.Fatalf("no-expiry key: %+v", k)
	}
}

func testDuplicates(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	if err := ks.CreateKey(ctx, mkKey("k1", "one", "tok", auth.Read)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		key  auth.Key
		want error
	}{
		{"name", mkKey("k2", "one", "other", auth.Read), auth.ErrDuplicateName},
		{"id", mkKey("k1", "two", "other", auth.Admin), auth.ErrDuplicateKey},
		{"digest", mkKey("k2", "two", "tok", auth.Admin), auth.ErrDuplicateKey},
		{"name wins over id", mkKey("k1", "one", "other", auth.Read), auth.ErrDuplicateName},
		{"name wins over digest", mkKey("k2", "one", "tok", auth.Read), auth.ErrDuplicateName},
	} {
		if err := ks.CreateKey(ctx, c.key); !errors.Is(err, c.want) {
			t.Errorf("duplicate %s: got %v, want %v", c.name, err, c.want)
		}
	}
	// The original must be untouched by every rejected attempt.
	list, _ := ks.ListKeys(ctx)
	if len(list) != 1 || list[0].Scope != auth.Read || list[0].Name != "one" {
		t.Fatalf("store mutated by rejected creates: %+v", list)
	}
}

func testListKeys(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	if list, err := ks.ListKeys(ctx); err != nil || len(list) != 0 {
		t.Fatalf("empty list: %v %v", list, err)
	}
	b := mkKey("b", "b", "tb", auth.Read)
	b.CreatedAt = t0.Add(time.Second)
	a := mkKey("a", "a", "ta", auth.Read)
	a.CreatedAt = t0.Add(time.Second)
	c := mkKey("c", "c", "tc", auth.Read)
	for _, k := range []auth.Key{b, a, c} {
		if err := ks.CreateKey(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	list, err := ks.ListKeys(ctx)
	if err != nil || len(list) != 3 {
		t.Fatalf("list: %v %v", list, err)
	}
	if list[0].ID != "c" || list[1].ID != "a" || list[2].ID != "b" {
		t.Fatalf("order by created_at then id: %v %v %v", list[0].ID, list[1].ID, list[2].ID)
	}
	for _, k := range list {
		if k.Digest != ([sha256.Size]byte{}) {
			t.Fatalf("ListKeys must zero digests: %+v", k)
		}
	}
}

func testRevoke(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	if err := ks.RevokeKey(ctx, "nope"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("revoke unknown: %v", err)
	}
	_ = ks.CreateKey(ctx, mkKey("a", "a", "ta", auth.Read))
	before := time.Now().Add(-time.Second)
	if err := ks.RevokeKey(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	k, err := ks.LookupKey(ctx, digestOf("ta"))
	if err != nil || !k.Revoked() || k.RevokedAt.Before(before) || k.RevokedAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("revoked lookup: %+v %v", k, err)
	}
	first := k.RevokedAt
	if err := ks.RevokeKey(ctx, "a"); !errors.Is(err, auth.ErrAlreadyRevoked) {
		t.Fatalf("revoke twice: %v", err)
	}
	if k, _ := ks.LookupKey(ctx, digestOf("ta")); !k.RevokedAt.Equal(first) {
		t.Fatal("second revoke must not move revoked_at")
	}
	// A revoked key still occupies its name, id, and digest.
	if err := ks.CreateKey(ctx, mkKey("b", "a", "tb", auth.Read)); !errors.Is(err, auth.ErrDuplicateName) {
		t.Fatalf("reuse revoked name: %v", err)
	}
	if err := ks.CreateKey(ctx, mkKey("b", "b", "ta", auth.Read)); !errors.Is(err, auth.ErrDuplicateKey) {
		t.Fatalf("reuse revoked digest: %v", err)
	}
	if list, _ := ks.ListKeys(ctx); len(list) != 1 || !list[0].Revoked() {
		t.Fatalf("revoked keys are still listed: %+v", list)
	}
}

func testTouch(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	if err := ks.TouchKey(ctx, "nope", t0); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("touch unknown: %v", err)
	}
	_ = ks.CreateKey(ctx, mkKey("a", "a", "ta", auth.Read))
	at := t0.Add(time.Hour)
	if err := ks.TouchKey(ctx, "a", at); err != nil {
		t.Fatal(err)
	}
	k, _ := ks.LookupKey(ctx, digestOf("ta"))
	if !k.LastUsedAt.Equal(at) {
		t.Fatalf("last used %v want %v", k.LastUsedAt, at)
	}
	// Touching a revoked key is allowed and does not un-revoke it.
	_ = ks.RevokeKey(ctx, "a")
	if err := ks.TouchKey(ctx, "a", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if k, _ := ks.LookupKey(ctx, digestOf("ta")); !k.Revoked() {
		t.Fatal("touch must not clear revocation")
	}
}

func testAudit(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	if ev, err := ks.ListAudit(ctx, auth.AuditFilter{}); err != nil || len(ev) != 0 {
		t.Fatalf("empty audit: %v %v", ev, err)
	}
	acts := []string{auth.ActionKeyCreate, auth.ActionKeyRevoke, auth.ActionAuditPrune}
	// Appended out of chronological order to prove ordering is by At.
	for _, i := range []int{1, 0, 2} {
		e := auth.Event{ID: fmt.Sprintf("e%d", i), At: t0.Add(time.Duration(i) * time.Hour),
			Principal: "admin", Action: acts[i], Target: "k"}
		if i == 0 {
			e.RequestID = "req-1"
		}
		if err := ks.AppendAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	all, err := ks.ListAudit(ctx, auth.AuditFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("list: %v %v", all, err)
	}
	if all[0].ID != "e2" || all[1].ID != "e1" || all[2].ID != "e0" {
		t.Fatalf("newest first: %+v", all)
	}
	if all[2].RequestID != "req-1" || all[1].RequestID != "" {
		t.Fatalf("RequestID roundtrip: %+v", all)
	}
	if !all[2].At.Equal(t0) || all[2].Principal != "admin" || all[2].Action != auth.ActionKeyCreate || all[2].Target != "k" {
		t.Fatalf("fields roundtrip: %+v", all[2])
	}
	if lim, _ := ks.ListAudit(ctx, auth.AuditFilter{Limit: 2}); len(lim) != 2 || lim[0].ID != "e2" {
		t.Fatalf("limit: %+v", lim)
	}
	if lim, _ := ks.ListAudit(ctx, auth.AuditFilter{Limit: -1}); len(lim) != 3 {
		t.Fatalf("negative limit means none: %+v", lim)
	}
	if since, _ := ks.ListAudit(ctx, auth.AuditFilter{Since: t0.Add(time.Hour)}); len(since) != 2 {
		t.Fatalf("since is inclusive: %+v", since)
	}
	if since, _ := ks.ListAudit(ctx, auth.AuditFilter{Since: t0.Add(time.Hour + time.Nanosecond)}); len(since) != 1 {
		t.Fatalf("since excludes earlier by a nanosecond: %+v", since)
	}
}

func testPrune(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	if n, err := ks.PruneAudit(ctx, t0); err != nil || n != 0 {
		t.Fatalf("prune empty: %d %v", n, err)
	}
	// Sub-second and trailing-zero timestamps must compare chronologically.
	ats := []time.Time{
		t0.Truncate(time.Second),
		t0.Truncate(time.Second).Add(500 * time.Millisecond),
		t0.Truncate(time.Second).Add(time.Second),
		t0.Add(time.Hour),
	}
	for i, at := range ats {
		_ = ks.AppendAudit(ctx, auth.Event{ID: fmt.Sprintf("e%d", i), At: at, Principal: "p", Action: auth.ActionKeyCreate, Target: "k"})
	}
	// Boundary: an event exactly at `before` is kept.
	n, err := ks.PruneAudit(ctx, ats[2])
	if err != nil || n != 2 {
		t.Fatalf("pruned %d (%v), want 2", n, err)
	}
	rest, _ := ks.ListAudit(ctx, auth.AuditFilter{})
	if len(rest) != 2 || rest[1].ID != "e2" {
		t.Fatalf("after prune: %+v", rest)
	}
	if n, _ := ks.PruneAudit(ctx, ats[2]); n != 0 {
		t.Fatalf("prune is idempotent, got %d", n)
	}
}

func testConcurrentCreate(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	const n = 16
	var ok, dupName, other atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch err := ks.CreateKey(ctx, mkKey(fmt.Sprintf("id%d", i), "same", fmt.Sprintf("tok%d", i), auth.Read)); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, auth.ErrDuplicateName):
				dupName.Add(1)
			default:
				other.Add(1)
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || dupName.Load() != n-1 {
		t.Fatalf("ok=%d dupName=%d other=%d", ok.Load(), dupName.Load(), other.Load())
	}
	if list, _ := ks.ListKeys(ctx); len(list) != 1 {
		t.Fatalf("want exactly one key, got %d", len(list))
	}
}

func testConcurrentRevoke(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	_ = ks.CreateKey(ctx, mkKey("a", "a", "ta", auth.Read))
	const n = 16
	var ok, already atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch err := ks.RevokeKey(ctx, "a"); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, auth.ErrAlreadyRevoked):
				already.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || already.Load() != n-1 {
		t.Fatalf("ok=%d already=%d", ok.Load(), already.Load())
	}
}

func testConcurrentMixed(t *testing.T, ks auth.KeyStore) {
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		_ = ks.CreateKey(ctx, mkKey(fmt.Sprintf("k%d", i), fmt.Sprintf("n%d", i), fmt.Sprintf("t%d", i), auth.Read))
	}
	const workers, per = 8, 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < per; j++ {
				id := fmt.Sprintf("k%d", j%4)
				if err := ks.TouchKey(ctx, id, t0.Add(time.Duration(j)*time.Second)); err != nil {
					t.Errorf("touch: %v", err)
				}
				if _, err := ks.LookupKey(ctx, digestOf(fmt.Sprintf("t%d", j%4))); err != nil {
					t.Errorf("lookup: %v", err)
				}
				if err := ks.AppendAudit(ctx, auth.Event{ID: fmt.Sprintf("e-%d-%d", w, j), At: t0.Add(time.Duration(j) * time.Second),
					Principal: "p", Action: auth.ActionKeyCreate, Target: id}); err != nil {
					t.Errorf("append: %v", err)
				}
				if _, err := ks.ListAudit(ctx, auth.AuditFilter{Limit: 5}); err != nil {
					t.Errorf("list audit: %v", err)
				}
				if _, err := ks.PruneAudit(ctx, t0.Add(-time.Hour)); err != nil {
					t.Errorf("prune: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	all, err := ks.ListAudit(ctx, auth.AuditFilter{})
	if err != nil || len(all) != workers*per {
		t.Fatalf("audit count %d (%v), want %d", len(all), err, workers*per)
	}
}
