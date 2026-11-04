package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func digestOf(tok string) [sha256.Size]byte { return sha256.Sum256([]byte(tok)) }

func mkKey(id, name, tok string, sc Scope) Key {
	return Key{ID: id, Name: name, Digest: digestOf(tok), Scope: sc, CreatedAt: t0}
}

// clock is a settable test clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

func newChain(t *testing.T, ks KeyStore, c *clock) *Chain {
	t.Helper()
	st := NewStatic(map[Scope][]string{Read: {"static-read"}, Admin: {"shared"}})
	return NewChain(st, ks, WithClock(c.now))
}

func TestChainStaticFirstAndKeyID(t *testing.T) {
	ks := NewMemKeyStore()
	_ = ks.CreateKey(context.Background(), mkKey("k1", "ci", "managed-tok", Write))
	ch := newChain(t, ks, &clock{t: t0})

	p, err := ch.Authenticate(req("Bearer static-read"))
	if err != nil || p.Scope != Read || p.KeyID != "" {
		t.Fatalf("static hit: %+v %v", p, err)
	}
	p, err = ch.Authenticate(req("Bearer managed-tok"))
	if err != nil || p.Scope != Write || p.Name != "ci" || p.KeyID != "k1" {
		t.Fatalf("managed hit: %+v %v", p, err)
	}
}

func TestChainStaticTakesPrecedence(t *testing.T) {
	ks := NewMemKeyStore()
	// A managed key with the same token as a static admin token, at lower scope.
	_ = ks.CreateKey(context.Background(), mkKey("k1", "dup", "shared", Eval))
	ch := newChain(t, ks, &clock{t: t0})
	p, err := ch.Authenticate(req("Bearer shared"))
	if err != nil || p.Scope != Admin || p.KeyID != "" {
		t.Fatalf("static must win: %+v %v", p, err)
	}
	if ks.keys["k1"].LastUsedAt != (time.Time{}) {
		t.Error("static hit must not touch the key store")
	}
}

func TestChainRevokedExpiredUnknownIndistinguishable(t *testing.T) {
	ctx := context.Background()
	ks := NewMemKeyStore()
	clk := &clock{t: t0}
	rev := mkKey("r", "revoked", "rev-tok", Admin)
	_ = ks.CreateKey(ctx, rev)
	_ = ks.RevokeKey(ctx, "r")
	exp := mkKey("e", "expired", "exp-tok", Admin)
	exp.ExpiresAt = t0.Add(-time.Hour)
	_ = ks.CreateKey(ctx, exp)
	ch := newChain(t, ks, clk)

	var errs []error
	for _, h := range []string{"Bearer rev-tok", "Bearer exp-tok", "Bearer nope", "Bearer ", "Basic abc", ""} {
		p, err := ch.Authenticate(req(h))
		if p != (Principal{}) {
			t.Errorf("%q: principal leaked %+v", h, p)
		}
		errs = append(errs, err)
	}
	for i, err := range errs {
		if err != ErrUnauthenticated { // identity, not just errors.Is
			t.Errorf("case %d: got %v want ErrUnauthenticated", i, err)
		}
	}
	// Same through the digest path used by sessions.
	for _, tok := range []string{"rev-tok", "exp-tok", "nope"} {
		if _, err := ch.AuthenticateDigest(digestOf(tok)); err != ErrUnauthenticated {
			t.Errorf("digest %s: %v", tok, err)
		}
	}
}

func TestChainExpiryBoundary(t *testing.T) {
	ks := NewMemKeyStore()
	k := mkKey("k", "n", "tok", Read)
	k.ExpiresAt = t0.Add(time.Hour)
	_ = ks.CreateKey(context.Background(), k)
	clk := &clock{t: k.ExpiresAt.Add(-time.Nanosecond)}
	ch := newChain(t, ks, clk)

	if _, err := ch.AuthenticateDigest(digestOf("tok")); err != nil {
		t.Fatalf("just before expiry: %v", err)
	}
	clk.set(k.ExpiresAt) // now == expires_at: expired
	if _, err := ch.AuthenticateDigest(digestOf("tok")); err != ErrUnauthenticated {
		t.Fatalf("at expiry: %v", err)
	}
	clk.set(k.ExpiresAt.Add(time.Nanosecond))
	if _, err := ch.AuthenticateDigest(digestOf("tok")); err != ErrUnauthenticated {
		t.Fatalf("after expiry: %v", err)
	}
}

func TestChainRevocationTakesEffectLive(t *testing.T) {
	ctx := context.Background()
	ks := NewMemKeyStore()
	_ = ks.CreateKey(ctx, mkKey("k", "n", "tok", Read))
	ch := newChain(t, ks, &clock{t: t0})
	if _, err := ch.AuthenticateDigest(digestOf("tok")); err != nil {
		t.Fatal(err)
	}
	if err := ks.RevokeKey(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := ch.AuthenticateDigest(digestOf("tok")); err != ErrUnauthenticated {
		t.Fatalf("revoked key still authenticates: %v", err)
	}
}

func TestChainNoKeyStoreFallback(t *testing.T) {
	st := NewStatic(map[Scope][]string{Read: {"static-read"}})
	ch := NewChain(st, nil)
	if p, err := ch.Authenticate(req("Bearer static-read")); err != nil || p.Scope != Read {
		t.Fatalf("static fallback: %+v %v", p, err)
	}
	if _, err := ch.Authenticate(req("Bearer other")); err != ErrUnauthenticated {
		t.Fatalf("got %v", err)
	}
	// Nil static and nil keys fails closed.
	if _, err := NewChain(nil, nil).Authenticate(req("Bearer x")); err != ErrUnauthenticated {
		t.Fatalf("empty chain: %v", err)
	}
}

// failingStore wraps MemKeyStore with injectable failures.
type failingStore struct {
	*MemKeyStore
	lookupErr error
	touchErr  error
	touches   int
	badDigest bool
}

func (f *failingStore) LookupKey(ctx context.Context, d [sha256.Size]byte) (Key, error) {
	if f.lookupErr != nil {
		return Key{}, f.lookupErr
	}
	k, err := f.MemKeyStore.LookupKey(ctx, d)
	if f.badDigest {
		k.Digest[0] ^= 0xff // a misbehaving store returning the wrong key
	}
	return k, err
}

func (f *failingStore) TouchKey(ctx context.Context, id string, at time.Time) error {
	f.touches++
	if f.touchErr != nil {
		return f.touchErr
	}
	return f.MemKeyStore.TouchKey(ctx, id, at)
}

func TestChainFailsClosed(t *testing.T) {
	ctx := context.Background()
	fs := &failingStore{MemKeyStore: NewMemKeyStore()}
	_ = fs.CreateKey(ctx, mkKey("k", "n", "tok", Read))
	ch := newChain(t, fs, &clock{t: t0})

	fs.lookupErr = errors.New("db down")
	if _, err := ch.AuthenticateDigest(digestOf("tok")); err != ErrUnauthenticated {
		t.Errorf("store error must fail closed: %v", err)
	}
	fs.lookupErr = nil
	fs.badDigest = true
	if _, err := ch.AuthenticateDigest(digestOf("tok")); err != ErrUnauthenticated {
		t.Errorf("digest mismatch from store must fail closed: %v", err)
	}
	fs.badDigest = false

	bad := mkKey("b", "bad", "bad-tok", Scope(0))
	_ = fs.CreateKey(ctx, bad)
	if _, err := ch.AuthenticateDigest(digestOf("bad-tok")); err != ErrUnauthenticated {
		t.Errorf("invalid scope must fail closed: %v", err)
	}
}

func TestChainLastUsedThrottled(t *testing.T) {
	ctx := context.Background()
	fs := &failingStore{MemKeyStore: NewMemKeyStore()}
	_ = fs.CreateKey(ctx, mkKey("k", "n", "tok", Read))
	clk := &clock{t: t0}
	ch := newChain(t, fs, clk)
	d := digestOf("tok")

	for i := 0; i < 5; i++ {
		if _, err := ch.AuthenticateDigest(d); err != nil {
			t.Fatal(err)
		}
	}
	if fs.touches != 1 {
		t.Fatalf("touches=%d want 1", fs.touches)
	}
	if got := fs.keys["k"].LastUsedAt; !got.Equal(t0) {
		t.Fatalf("last_used_at=%v want %v", got, t0)
	}
	clk.set(t0.Add(touchInterval - time.Second))
	_, _ = ch.AuthenticateDigest(d)
	if fs.touches != 1 {
		t.Fatalf("touched inside interval: %d", fs.touches)
	}
	clk.set(t0.Add(touchInterval))
	_, _ = ch.AuthenticateDigest(d)
	if fs.touches != 2 {
		t.Fatalf("touches=%d want 2", fs.touches)
	}
	if got := fs.keys["k"].LastUsedAt; !got.Equal(t0.Add(touchInterval)) {
		t.Fatalf("last_used_at=%v", got)
	}
}

func TestChainTouchFailureDoesNotFailRequest(t *testing.T) {
	ctx := context.Background()
	fs := &failingStore{MemKeyStore: NewMemKeyStore(), touchErr: errors.New("locked")}
	_ = fs.CreateKey(ctx, mkKey("k", "n", "tok", Read))
	ch := newChain(t, fs, &clock{t: t0})
	if p, err := ch.AuthenticateDigest(digestOf("tok")); err != nil || p.KeyID != "k" {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestChainConcurrent(t *testing.T) {
	ks := NewMemKeyStore()
	_ = ks.CreateKey(context.Background(), mkKey("k", "n", "tok", Read))
	ch := newChain(t, ks, &clock{t: t0})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := ch.AuthenticateDigest(digestOf("tok")); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
