package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseScope(t *testing.T) {
	for in, want := range map[string]Scope{"eval": Eval, " Read ": Read, "WRITE": Write, "admin": Admin} {
		if got, err := ParseScope(in); err != nil || got != want {
			t.Errorf("ParseScope(%q) = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "root", "adm"} {
		if _, err := ParseScope(in); err == nil {
			t.Errorf("ParseScope(%q) succeeded", in)
		}
	}
}

func TestCreateManagedKey(t *testing.T) {
	ctx := context.Background()
	ks := NewMemKeyStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	res, err := CreateManagedKey(ctx, ks, NewKey{Name: " bot ", Scope: Write, Principal: "p", RequestID: "r1", Ceiling: Admin, Now: now})
	if err != nil || res.AuditErr != nil {
		t.Fatalf("create: %v %v", err, res.AuditErr)
	}
	if res.Key.Name != "bot" || !strings.HasPrefix(res.Token, TokenPrefix) {
		t.Errorf("result = %+v", res)
	}
	got, err := ks.LookupKey(ctx, sha256.Sum256([]byte(res.Token)))
	if err != nil || got.ID != res.Key.ID {
		t.Errorf("token does not resolve to the stored key: %v", err)
	}
	evs, _ := ks.ListAudit(ctx, AuditFilter{})
	if len(evs) != 1 || evs[0].Principal != "p" || evs[0].RequestID != "r1" || evs[0].Target != res.Key.ID || !evs[0].At.Equal(now) {
		t.Errorf("audit = %+v", evs)
	}

	bad := []struct {
		name string
		n    NewKey
		want error
	}{
		{"no name", NewKey{Scope: Read, Now: now}, ErrInvalidKey},
		{"long name", NewKey{Name: strings.Repeat("x", MaxKeyNameLen+1), Scope: Read, Now: now}, ErrInvalidKey},
		{"no scope", NewKey{Name: "a", Now: now}, ErrInvalidKey},
		{"expiry now", NewKey{Name: "a", Scope: Read, ExpiresAt: now, Now: now}, ErrInvalidKey},
		{"ceiling", NewKey{Name: "a", Scope: Admin, Ceiling: Write, Now: now}, ErrScopeCeiling},
		{"duplicate", NewKey{Name: "bot", Scope: Read, Now: now}, ErrDuplicateName},
	}
	for _, c := range bad {
		if _, err := CreateManagedKey(ctx, ks, c.n); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if keys, _ := ks.ListKeys(ctx); len(keys) != 1 {
		t.Errorf("rejected requests stored %d keys", len(keys)-1)
	}
}

func TestRevokeManagedKey(t *testing.T) {
	ctx := context.Background()
	ks := NewMemKeyStore()
	now := time.Now().UTC()
	res, _ := CreateManagedKey(ctx, ks, NewKey{Name: "a", Scope: Read, Now: now})
	if aerr, err := RevokeManagedKey(ctx, ks, res.Key.ID, "p", "r2", now); err != nil || aerr != nil {
		t.Fatalf("revoke: %v %v", err, aerr)
	}
	if _, err := RevokeManagedKey(ctx, ks, res.Key.ID, "p", "", now); !errors.Is(err, ErrAlreadyRevoked) {
		t.Errorf("second revoke = %v", err)
	}
	if _, err := RevokeManagedKey(ctx, ks, "nope", "p", "", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown revoke = %v", err)
	}
	evs, _ := ks.ListAudit(ctx, AuditFilter{})
	if len(evs) != 2 {
		t.Fatalf("audit = %+v (failed revokes must not be audited)", evs)
	}
}
