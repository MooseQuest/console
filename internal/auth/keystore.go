package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"sync"
	"time"
)

// Typed errors returned by KeyStore implementations. They are for admin-facing
// callers (CLI, admin API); Authenticate never exposes them and returns only
// ErrUnauthenticated.
var (
	// ErrNotFound means no key matches the id, name, or digest.
	ErrNotFound = errors.New("auth: not found")
	// ErrDuplicateName means a key with the same name already exists.
	ErrDuplicateName = errors.New("auth: duplicate key name")
	// ErrAlreadyRevoked means the key was revoked before this call.
	ErrAlreadyRevoked = errors.New("auth: key already revoked")
)

// Audit actions recorded in Event.Action. Event.Target is the key id.
const (
	ActionKeyCreate  = "key.create"
	ActionKeyRevoke  = "key.revoke"
	ActionAuditPrune = "audit.prune"
)

// Key is a managed API key. Only the SHA-256 digest of the token is stored;
// the plaintext token is shown once at creation and never retained.
type Key struct {
	// ID is a random 128-bit hex identifier.
	ID string
	// Name is the operator-chosen unique label; it becomes Principal.Name.
	Name string
	// Digest is the SHA-256 of the token. ListKeys returns it zeroed.
	Digest [sha256.Size]byte
	Scope  Scope

	CreatedAt time.Time
	// ExpiresAt, when non-zero, is the instant the key stops working: the key
	// is expired when now >= ExpiresAt.
	ExpiresAt time.Time
	// LastUsedAt is zero until the key first authenticates.
	LastUsedAt time.Time
	// RevokedAt is non-zero once the key has been revoked.
	RevokedAt time.Time
}

// Revoked reports whether the key has been revoked.
func (k Key) Revoked() bool { return !k.RevokedAt.IsZero() }

// Expired reports whether the key is expired at now (now >= ExpiresAt).
func (k Key) Expired(now time.Time) bool {
	return !k.ExpiresAt.IsZero() && !now.Before(k.ExpiresAt)
}

// Event is one audit record of a key-management action.
type Event struct {
	// ID is a random 128-bit hex identifier.
	ID string
	At time.Time
	// Principal is the Name of the acting principal.
	Principal string
	// Action is one of the Action* constants.
	Action string
	// Target is the id of the key acted on (empty for audit.prune).
	Target string
	// RequestID optionally correlates the event with the request that caused it.
	RequestID string
}

// AuditFilter selects audit events. Results are newest first.
type AuditFilter struct {
	// Since, when non-zero, excludes events before it.
	Since time.Time
	// Limit caps the result count; zero or negative means no limit.
	Limit int
}

// KeyStore is the optional persistence seam for managed API keys and their
// audit trail. It is separate from store.Store so plugin stores that do not
// implement it fall back to env-defined tokens only.
type KeyStore interface {
	// CreateKey stores a fully populated Key (digest only). It returns
	// ErrDuplicateName if the name is taken.
	CreateKey(ctx context.Context, k Key) error
	// LookupKey returns the key with the given digest, including revoked and
	// expired keys, or ErrNotFound. The caller decides whether it is usable.
	LookupKey(ctx context.Context, digest [sha256.Size]byte) (Key, error)
	// ListKeys returns all keys with Digest zeroed, so nothing returned is
	// usable as a credential.
	ListKeys(ctx context.Context) ([]Key, error)
	// RevokeKey marks the key revoked. It returns ErrNotFound for an unknown id
	// and ErrAlreadyRevoked if it was already revoked.
	RevokeKey(ctx context.Context, id string) error
	// TouchKey records the last-used time of a key.
	TouchKey(ctx context.Context, id string, at time.Time) error

	// AppendAudit records an audit event.
	AppendAudit(ctx context.Context, e Event) error
	// ListAudit returns events matching f, newest first.
	ListAudit(ctx context.Context, f AuditFilter) ([]Event, error)
	// PruneAudit deletes events older than before and returns how many.
	PruneAudit(ctx context.Context, before time.Time) (int, error)
}

// MemKeyStore is an in-memory KeyStore, intended for tests and as a reference
// implementation. It is safe for concurrent use.
type MemKeyStore struct {
	mu     sync.Mutex
	keys   map[string]Key // by id
	events []Event
}

// NewMemKeyStore returns an empty in-memory KeyStore.
func NewMemKeyStore() *MemKeyStore {
	return &MemKeyStore{keys: map[string]Key{}}
}

var _ KeyStore = (*MemKeyStore)(nil)

// CreateKey implements KeyStore.
func (m *MemKeyStore) CreateKey(_ context.Context, k Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.keys {
		if e.Name == k.Name {
			return ErrDuplicateName
		}
	}
	m.keys[k.ID] = k
	return nil
}

// LookupKey implements KeyStore.
func (m *MemKeyStore) LookupKey(_ context.Context, d [sha256.Size]byte) (Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.keys {
		if k.Digest == d {
			return k, nil
		}
	}
	return Key{}, ErrNotFound
}

// ListKeys implements KeyStore. Keys are ordered by creation time, then id.
func (m *MemKeyStore) ListKeys(context.Context) ([]Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Key, 0, len(m.keys))
	for _, k := range m.keys {
		k.Digest = [sha256.Size]byte{}
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// RevokeKey implements KeyStore. The revocation time is the wall clock.
func (m *MemKeyStore) RevokeKey(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[id]
	if !ok {
		return ErrNotFound
	}
	if k.Revoked() {
		return ErrAlreadyRevoked
	}
	k.RevokedAt = time.Now().UTC()
	m.keys[id] = k
	return nil
}

// TouchKey implements KeyStore.
func (m *MemKeyStore) TouchKey(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[id]
	if !ok {
		return ErrNotFound
	}
	k.LastUsedAt = at
	m.keys[id] = k
	return nil
}

// AppendAudit implements KeyStore.
func (m *MemKeyStore) AppendAudit(_ context.Context, e Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

// ListAudit implements KeyStore.
func (m *MemKeyStore) ListAudit(_ context.Context, f AuditFilter) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Event
	for _, e := range m.events {
		if !f.Since.IsZero() && e.At.Before(f.Since) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// PruneAudit implements KeyStore.
func (m *MemKeyStore) PruneAudit(_ context.Context, before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []Event
	n := 0
	for _, e := range m.events {
		if e.At.Before(before) {
			n++
			continue
		}
		kept = append(kept, e)
	}
	m.events = kept
	return n, nil
}
