package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/store/sqlite"
)

type harness struct {
	t   *testing.T
	ks  auth.KeyStore
	now time.Time
	env authEnv
	err bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "console.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &harness{t: t, ks: st, now: time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)}
	h.env = authEnv{
		Keys:          func() (auth.KeyStore, error) { return st, nil },
		RetentionDays: 90,
		Now:           func() time.Time { return h.now },
		Err:           &h.err,
	}
	return h
}

func (h *harness) run(args ...string) (string, error) {
	h.t.Helper()
	var out bytes.Buffer
	err := runAuth(context.Background(), args, &out, h.env)
	return out.String(), err
}

func isUsage(err error) bool {
	var ue *usageError
	return errors.As(err, &ue)
}

func wantUsage(t *testing.T, err error) {
	t.Helper()
	if !isUsage(err) {
		t.Fatalf("want usage error, got %v", err)
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"90d": 90 * 24 * time.Hour, "1d": 24 * time.Hour, "0": 0, "0d": 0,
		"36h": 36 * time.Hour, "1h30m": 90 * time.Minute, "-2d": -48 * time.Hour,
	} {
		got, err := parseDuration(in)
		if err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "xd", "1.5d", "abc", "10"} {
		if _, err := parseDuration(in); err == nil {
			t.Errorf("parseDuration(%q): want error", in)
		}
	}
}

func TestKeyCreatePrintsTokenOnceAndStoresDigest(t *testing.T) {
	h := newHarness(t)
	out, err := h.run("key", "create", "ci", "-scope", "read", "-expires-in", "30d")
	if err != nil {
		t.Fatal(err)
	}
	tok := strings.TrimSpace(out)
	if !strings.HasPrefix(tok, auth.TokenPrefix) || strings.Count(out, "\n") != 1 {
		t.Fatalf("stdout must be exactly the token, got %q", out)
	}
	if strings.Contains(h.err.String(), tok) {
		t.Error("token leaked to the notes stream")
	}
	k, err := h.ks.LookupKey(context.Background(), sha256.Sum256([]byte(tok)))
	if err != nil {
		t.Fatal(err)
	}
	if k.Name != "ci" || k.Scope != auth.Read || !k.ExpiresAt.Equal(h.now.Add(30*24*time.Hour)) || !k.CreatedAt.Equal(h.now) {
		t.Errorf("unexpected key %+v", k)
	}
	// The token never reappears in list output (table or JSON).
	for _, args := range [][]string{{"key", "list"}, {"key", "list", "-json"}} {
		lo, err := h.run(args...)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(lo, tok) || strings.Contains(strings.ToLower(lo), "digest") {
			t.Errorf("list leaked secret material: %s", lo)
		}
	}
	evs, _ := h.ks.ListAudit(context.Background(), auth.AuditFilter{})
	if len(evs) != 1 || evs[0].Action != auth.ActionKeyCreate || evs[0].Principal != "cli" || evs[0].Target != k.ID {
		t.Errorf("audit = %+v", evs)
	}
}

func TestKeyUsageErrors(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"key", "create"},
		{"key", "create", "x"},
		{"key", "create", "x", "-scope", "root"},
		{"key", "create", "x", "-scope", "read", "-expires-in", "0"},
		{"key", "create", "x", "-scope", "read", "-expires-in", "-1d"},
		{"key", "create", "x", "-scope", "read", "-expires-in", "soon"},
		{"key", "create", "x", "-scope", "read", "extra"},
		{"key", "create", "x", "-bogus"},
		{"key", "bogus"},
		{"key", "revoke"},
		{"key", "revoke", "a", "b"},
		{"key", "list", "extra"},
		{"audit", "bogus"},
	} {
		if _, err := h.run(args...); !isUsage(err) {
			t.Errorf("%v: want usage error, got %v", args, err)
		}
	}
	if keys, _ := h.ks.ListKeys(context.Background()); len(keys) != 0 {
		t.Errorf("usage errors must not create keys: %+v", keys)
	}
	// A duplicate name is a runtime error.
	if _, err := h.run("key", "create", "dup", "-scope", "eval"); err != nil {
		t.Fatal(err)
	}
	_, err := h.run("key", "create", "dup", "-scope", "eval")
	if err == nil || isUsage(err) || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate: got %v", err)
	}
}

func TestKeyRevokeByIDAndName(t *testing.T) {
	h := newHarness(t)
	shared := auth.NewMemKeyStore()
	h.env.Keys = func() (auth.KeyStore, error) { return shared, nil }
	h.ks = shared

	tokA, _ := h.run("key", "create", "alpha", "-scope", "write")
	tokB, _ := h.run("key", "create", "beta", "-scope", "read")
	keys, _ := shared.ListKeys(context.Background())
	byName := map[string]auth.Key{}
	for _, k := range keys {
		byName[k.Name] = k
	}

	// A Chain over the same store models a running server.
	chain := auth.NewChain(nil, shared)
	authed := func(tok string) bool {
		_, err := chain.AuthenticateDigest(sha256.Sum256([]byte(strings.TrimSpace(tok))))
		return err == nil
	}
	if !authed(tokA) || !authed(tokB) {
		t.Fatal("new keys must authenticate")
	}

	if _, err := h.run("key", "revoke", byName["alpha"].ID); err != nil { // by id
		t.Fatal(err)
	}
	if authed(tokA) {
		t.Error("revoked-by-id key still authenticates")
	}
	if !authed(tokB) {
		t.Error("revoking alpha must not affect beta")
	}
	if _, err := h.run("key", "revoke", "beta"); err != nil { // by name
		t.Fatal(err)
	}
	if authed(tokB) {
		t.Error("revoked-by-name key still authenticates")
	}

	_, err := h.run("key", "revoke", "beta")
	if err == nil || !strings.Contains(err.Error(), "already revoked") || isUsage(err) {
		t.Errorf("already revoked: %v", err)
	}
	_, err = h.run("key", "revoke", "nope")
	if err == nil || !strings.Contains(err.Error(), "no key") || isUsage(err) {
		t.Errorf("unknown: %v", err)
	}

	evs, _ := shared.ListAudit(context.Background(), auth.AuditFilter{})
	revokes := 0
	for _, e := range evs {
		if e.Action == auth.ActionKeyRevoke {
			revokes++
			if e.Principal != "cli" || (e.Target != byName["alpha"].ID && e.Target != byName["beta"].ID) {
				t.Errorf("bad revoke event %+v", e)
			}
		}
	}
	if revokes != 2 {
		t.Errorf("revoke events = %d, want 2", revokes)
	}
}

func TestKeyRevokePrefersIDOverName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// Key two is named like key one's id; resolving "id-one" must pick the id match.
	h.ks.CreateKey(ctx, auth.Key{ID: "id-one", Name: "first", Digest: sha256.Sum256([]byte("1")), Scope: auth.Read, CreatedAt: h.now})
	h.ks.CreateKey(ctx, auth.Key{ID: "id-two", Name: "id-one", Digest: sha256.Sum256([]byte("2")), Scope: auth.Read, CreatedAt: h.now})
	if _, err := h.run("key", "revoke", "id-one"); err != nil {
		t.Fatal(err)
	}
	keys, _ := h.ks.ListKeys(ctx)
	for _, k := range keys {
		if (k.ID == "id-one") != k.Revoked() {
			t.Errorf("wrong key revoked: %+v", k)
		}
	}
}

func TestKeyListJSONNullsAndStatus(t *testing.T) {
	h := newHarness(t)
	if _, err := h.run("key", "create", "a", "-scope", "eval"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.run("key", "create", "b", "-scope", "admin", "-expires-in", "1h"); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(2 * time.Hour)
	out, err := h.run("key", "list", "-json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 {
		t.Fatalf("bad json %q: %v", out, err)
	}
	for _, r := range rows {
		if r["last_used_at"] != nil || r["revoked_at"] != nil {
			t.Errorf("absent timestamps must be null: %v", r)
		}
		switch r["name"] {
		case "a":
			if r["expires_at"] != nil || r["status"] != "active" {
				t.Errorf("a: %v", r)
			}
		case "b":
			if r["status"] != "expired" || r["expires_at"] != "2026-01-10T13:00:00Z" {
				t.Errorf("b: %v", r)
			}
		}
	}
	if o, _ := h.run("key", "list"); !strings.Contains(o, "NAME") || !strings.Contains(o, "expired") {
		t.Errorf("table: %s", o)
	}
}

func TestNoKeyStore(t *testing.T) {
	for _, keys := range []func() (auth.KeyStore, error){nil, func() (auth.KeyStore, error) { return nil, nil }} {
		for _, args := range [][]string{{"key", "list"}, {"key", "create", "x", "-scope", "read"}, {"audit", "list"}, {"audit", "prune", "-all"}} {
			var out bytes.Buffer
			err := runAuth(context.Background(), args, &out, authEnv{Keys: keys})
			if err == nil || !strings.Contains(err.Error(), "CONSOLE_*_TOKENS") || isUsage(err) {
				t.Errorf("%v: got %v", args, err)
			}
			if out.Len() != 0 {
				t.Errorf("%v: unexpected output %q", args, out.String())
			}
		}
	}
}

func seedAudit(h *harness, ages ...time.Duration) {
	for i, a := range ages {
		h.ks.AppendAudit(context.Background(), auth.Event{ID: string(rune('a' + i)), At: h.now.Add(-a), Principal: "x", Action: auth.ActionKeyCreate, Target: "t"})
	}
}

func auditState(h *harness) (n int, newest auth.Event) {
	evs, _ := h.ks.ListAudit(context.Background(), auth.AuditFilter{})
	if len(evs) > 0 {
		newest = evs[0]
	}
	return len(evs), newest
}

func TestAuditPrune(t *testing.T) {
	day := 24 * time.Hour
	t.Run("default uses configured retention", func(t *testing.T) {
		h := newHarness(t)
		seedAudit(h, 100*day, 91*day, 10*day)
		out, err := h.run("audit", "prune")
		if err != nil || !strings.Contains(out, "Pruned 2") {
			t.Fatalf("%q %v", out, err)
		}
		n, last := auditState(h)
		if n != 2 || last.Action != auth.ActionAuditPrune || last.Target != "2" || last.Principal != "cli" {
			t.Errorf("n=%d last=%+v", n, last)
		}
	})
	t.Run("older-than", func(t *testing.T) {
		h := newHarness(t)
		seedAudit(h, 10*day, 2*day, time.Hour)
		if _, err := h.run("audit", "prune", "-older-than", "7d"); err != nil {
			t.Fatal(err)
		}
		if n, _ := auditState(h); n != 3 { // two kept + the prune event
			t.Errorf("n=%d", n)
		}
	})
	t.Run("retention zero without flag is an error and deletes nothing", func(t *testing.T) {
		h := newHarness(t)
		h.env.RetentionDays = 0
		seedAudit(h, 1000*day)
		_, err := h.run("audit", "prune")
		wantUsage(t, err)
		if !strings.Contains(err.Error(), "-older-than") {
			t.Errorf("message should mention -older-than: %v", err)
		}
		if n, _ := auditState(h); n != 1 {
			t.Errorf("n=%d", n)
		}
	})
	t.Run("older-than 0 needs all", func(t *testing.T) {
		h := newHarness(t)
		seedAudit(h, day)
		for _, a := range []string{"0", "0d", "0h"} {
			_, err := h.run("audit", "prune", "-older-than", a)
			wantUsage(t, err)
		}
		if n, _ := auditState(h); n != 1 {
			t.Errorf("n=%d", n)
		}
		if _, err := h.run("audit", "prune", "-older-than", "0", "-all"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("all deletes everything but records itself", func(t *testing.T) {
		h := newHarness(t)
		seedAudit(h, day, time.Minute)
		h.env.RetentionDays = 0
		if _, err := h.run("audit", "prune", "-all"); err != nil {
			t.Fatal(err)
		}
		n, last := auditState(h)
		if n != 1 || last.Action != auth.ActionAuditPrune || last.Target != "2" {
			t.Errorf("n=%d last=%+v", n, last)
		}
	})
	t.Run("all with positive older-than is usage error", func(t *testing.T) {
		h := newHarness(t)
		seedAudit(h, 100*day)
		_, err := h.run("audit", "prune", "-all", "-older-than", "30d")
		wantUsage(t, err)
		if n, _ := auditState(h); n != 1 {
			t.Errorf("n=%d", n)
		}
	})
	t.Run("bad input", func(t *testing.T) {
		h := newHarness(t)
		for _, a := range [][]string{{"-older-than", "-5d"}, {"-older-than", "x"}, {"extra"}, {"-nope"}} {
			_, err := h.run(append([]string{"audit", "prune"}, a...)...)
			wantUsage(t, err)
		}
	})
}

func TestAuditList(t *testing.T) {
	h := newHarness(t)
	day := 24 * time.Hour
	seedAudit(h, 10*day, 2*day, time.Hour)
	out, err := h.run("audit", "list", "-since", "3d", "-json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 {
		t.Fatalf("%q %v", out, err)
	}
	out, _ = h.run("audit", "list", "-limit", "1")
	if strings.Count(out, "\n") != 2 { // header + 1
		t.Errorf("limit: %q", out)
	}
	for _, a := range [][]string{{"-since", "zzz"}, {"-since", "-1d"}, {"-limit", "-1"}, {"x"}} {
		_, err := h.run(append([]string{"audit", "list"}, a...)...)
		wantUsage(t, err)
	}
}

func TestAuthKeyHelp(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"key"}, {"key", "help"}, {"key", "create", "-h"}, {"key", "list", "--help"}, {"key", "revoke", "-h"}} {
		out, err := h.run(args...)
		if err != nil || !strings.Contains(out, "auth key create") {
			t.Errorf("%v: %q %v", args, out, err)
		}
	}
	for _, args := range [][]string{{"audit"}, {"audit", "help"}, {"audit", "prune", "-h"}, {"audit", "list", "-h"}} {
		out, err := h.run(args...)
		if err != nil || !strings.Contains(out, "auth audit prune") {
			t.Errorf("%v: %q %v", args, out, err)
		}
	}
}

// TestRunViaDB drives the real command path against a temp SQLite file.
func TestRunViaDB(t *testing.T) {
	t.Setenv("CONSOLE_DB", filepath.Join(t.TempDir(), "c.db"))
	t.Setenv("CONSOLE_AUDIT_RETENTION_DAYS", "0")
	c := &cli{}
	if err := c.run([]string{"auth", "key", "create", "svc", "-scope", "read"}); err != nil {
		t.Fatal(err)
	}
	if err := c.run([]string{"auth", "key", "revoke", "svc"}); err != nil {
		t.Fatal(err)
	}
	wantUsage(t, c.run([]string{"auth", "audit", "prune"}))
	if err := c.run([]string{"auth", "key", "revoke", "svc"}); err == nil || isUsage(err) {
		t.Errorf("want runtime error, got %v", err)
	}
}
