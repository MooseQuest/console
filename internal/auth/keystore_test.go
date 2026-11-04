package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestKeyExpired(t *testing.T) {
	k := Key{}
	if k.Expired(t0) {
		t.Error("zero ExpiresAt never expires")
	}
	k.ExpiresAt = t0
	if k.Expired(t0.Add(-1)) || !k.Expired(t0) || !k.Expired(t0.Add(1)) {
		t.Error("boundary: expired iff now >= ExpiresAt")
	}
}

func TestMemKeyStoreKeys(t *testing.T) {
	ctx := context.Background()
	ks := NewMemKeyStore()
	if err := ks.CreateKey(ctx, mkKey("a", "alpha", "ta", Read)); err != nil {
		t.Fatal(err)
	}
	if err := ks.CreateKey(ctx, mkKey("b", "alpha", "tb", Read)); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("dup name: %v", err)
	}
	if _, err := ks.LookupKey(ctx, digestOf("zzz")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lookup unknown: %v", err)
	}
	if k, err := ks.LookupKey(ctx, digestOf("ta")); err != nil || k.ID != "a" {
		t.Fatalf("lookup: %+v %v", k, err)
	}

	list, _ := ks.ListKeys(ctx)
	if len(list) != 1 || list[0].Digest != ([32]byte{}) {
		t.Fatalf("ListKeys must zero digests: %+v", list)
	}

	if err := ks.RevokeKey(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown: %v", err)
	}
	if err := ks.RevokeKey(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := ks.RevokeKey(ctx, "a"); !errors.Is(err, ErrAlreadyRevoked) {
		t.Fatalf("revoke twice: %v", err)
	}
	// Revoked keys are still returned by lookup (caller decides) and list.
	if k, err := ks.LookupKey(ctx, digestOf("ta")); err != nil || !k.Revoked() {
		t.Fatalf("revoked lookup: %+v %v", k, err)
	}
	if err := ks.TouchKey(ctx, "nope", t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("touch unknown: %v", err)
	}
}

func TestMemKeyStoreAudit(t *testing.T) {
	ctx := context.Background()
	ks := NewMemKeyStore()
	for i, a := range []string{ActionKeyCreate, ActionKeyRevoke, ActionAuditPrune} {
		_ = ks.AppendAudit(ctx, Event{ID: string(rune('a' + i)), At: t0.Add(time.Duration(i) * time.Hour),
			Principal: "admin", Action: a, Target: "k", RequestID: "req-1"})
	}
	all, _ := ks.ListAudit(ctx, AuditFilter{})
	if len(all) != 3 || all[0].Action != ActionAuditPrune || all[2].Action != ActionKeyCreate {
		t.Fatalf("newest first: %+v", all)
	}
	if all[0].RequestID != "req-1" {
		t.Error("RequestID not preserved")
	}
	lim, _ := ks.ListAudit(ctx, AuditFilter{Limit: 2})
	if len(lim) != 2 || lim[0].ID != "c" {
		t.Fatalf("limit: %+v", lim)
	}
	since, _ := ks.ListAudit(ctx, AuditFilter{Since: t0.Add(time.Hour)})
	if len(since) != 2 {
		t.Fatalf("since inclusive: %+v", since)
	}
	n, _ := ks.PruneAudit(ctx, t0.Add(time.Hour))
	if n != 1 {
		t.Fatalf("pruned %d want 1", n)
	}
	rest, _ := ks.ListAudit(ctx, AuditFilter{})
	if len(rest) != 2 {
		t.Fatalf("after prune: %+v", rest)
	}
}

func TestAuditActionVocabulary(t *testing.T) {
	if ActionKeyCreate != "key.create" || ActionKeyRevoke != "key.revoke" || ActionAuditPrune != "audit.prune" {
		t.Fatal("audit action constants changed")
	}
}

func TestMemKeyStoreRejectsDuplicateIDAndDigest(t *testing.T) {
	ctx := context.Background()
	ks := NewMemKeyStore()
	if err := ks.CreateKey(ctx, mkKey("k1", "one", "tok", Read)); err != nil {
		t.Fatal(err)
	}
	// Same id must not overwrite (it could resurrect a revoked key or change scope).
	if err := ks.CreateKey(ctx, mkKey("k1", "two", "other", Admin)); !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("duplicate id: got %v, want ErrDuplicateKey", err)
	}
	// Same digest must not give one token two keys.
	if err := ks.CreateKey(ctx, mkKey("k2", "three", "tok", Admin)); !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("duplicate digest: got %v, want ErrDuplicateKey", err)
	}
}
