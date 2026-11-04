package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// touchInterval is the minimum gap between last_used_at writes for one key, so
// authenticated reads do not turn into a database write per request.
const touchInterval = time.Minute

// Chain authenticates against static env-defined tokens first, then against
// managed keys in a KeyStore. It implements Authenticator and
// DigestAuthenticator. Unknown, revoked, and expired keys are
// indistinguishable to the caller: all yield ErrUnauthenticated.
type Chain struct {
	static *Static
	keys   KeyStore
	now    func() time.Time

	mu      sync.Mutex
	touched map[string]time.Time // key id -> last write attempt
}

// ChainOption customises a Chain.
type ChainOption func(*Chain)

// WithClock sets the clock used for expiry and last-used throttling. It exists
// so tests can pin the expiry boundary; the default is time.Now.
func WithClock(now func() time.Time) ChainOption {
	return func(c *Chain) { c.now = now }
}

// NewChain returns a Chain over static and keys. Either may be nil: a nil
// static matches nothing, and a nil keys behaves exactly like static alone.
func NewChain(static *Static, keys KeyStore, opts ...ChainOption) *Chain {
	c := &Chain{static: static, keys: keys, now: time.Now, touched: map[string]time.Time{}}
	for _, o := range opts {
		o(c)
	}
	return c
}

var (
	_ Authenticator       = (*Chain)(nil)
	_ DigestAuthenticator = (*Chain)(nil)
)

// Authenticate extracts "Authorization: Bearer <token>" and resolves it.
func (c *Chain) Authenticate(r *http.Request) (Principal, error) {
	h := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return Principal{}, ErrUnauthenticated
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return Principal{}, ErrUnauthenticated
	}
	return c.AuthenticateDigest(sha256.Sum256([]byte(tok)))
}

// AuthenticateDigest resolves a token digest: static tokens first, then the
// KeyStore. A successful managed-key hit refreshes last_used_at, throttled to
// once per minute per key; touch failures are logged and never fail the call.
func (c *Chain) AuthenticateDigest(d [sha256.Size]byte) (Principal, error) {
	if c.static != nil {
		if p, err := c.static.AuthenticateDigest(d); err == nil {
			return p, nil
		}
	}
	if c.keys == nil {
		return Principal{}, ErrUnauthenticated
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	k, err := c.keys.LookupKey(ctx, d)
	if err != nil {
		return Principal{}, ErrUnauthenticated // fail closed on any error
	}
	// Re-verify the digest in constant time rather than trusting the store.
	if subtle.ConstantTimeCompare(d[:], k.Digest[:]) != 1 {
		return Principal{}, ErrUnauthenticated
	}
	now := c.now()
	if k.Revoked() || k.Expired(now) || !k.Scope.Valid() {
		return Principal{}, ErrUnauthenticated
	}
	c.touch(ctx, k.ID, now)
	return Principal{Name: k.Name, Scope: k.Scope, KeyID: k.ID}, nil
}

func (c *Chain) touch(ctx context.Context, id string, now time.Time) {
	c.mu.Lock()
	if last, ok := c.touched[id]; ok && now.Sub(last) < touchInterval {
		c.mu.Unlock()
		return
	}
	c.touched[id] = now
	c.mu.Unlock()
	if err := c.keys.TouchKey(ctx, id, now); err != nil {
		slog.Warn("auth: update key last_used_at failed", "key_id", id, "err", err)
	}
}
