package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Errors returned by the key-management engine (CreateManagedKey) in addition
// to the KeyStore errors. They are for admin-facing callers (CLI, admin API,
// dashboard).
var (
	// ErrInvalidKey means the requested key is malformed: a missing or
	// oversized name, an invalid scope, or an expiry that is not in the future.
	// The error text is safe to show to the caller.
	ErrInvalidKey = errors.New("auth: invalid key")
	// ErrScopeCeiling means the requested scope is above the creator's scope.
	ErrScopeCeiling = errors.New("auth: requested scope exceeds the caller's scope")
)

// MaxKeyNameLen bounds a managed key's name, in bytes.
const MaxKeyNameLen = 128

// ParseScope parses a scope name ("eval", "read", "write", "admin"),
// case-insensitively and ignoring surrounding space.
func ParseScope(s string) (Scope, error) {
	for _, sc := range []Scope{Eval, Read, Write, Admin} {
		if sc.String() == strings.ToLower(strings.TrimSpace(s)) {
			return sc, nil
		}
	}
	return 0, fmt.Errorf("invalid scope %q: must be eval, read, write or admin", s)
}

// NewID returns a random 128-bit hex identifier, as used for key and audit
// event ids.
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// NewKey describes a key to create with CreateManagedKey.
type NewKey struct {
	Name  string
	Scope Scope
	// ExpiresAt, when non-zero, must be after Now.
	ExpiresAt time.Time
	// Principal is the audit label of the acting caller (e.g. "cli").
	Principal string
	// RequestID optionally correlates the audit event with an HTTP request.
	RequestID string
	// Ceiling, when valid, is the creator's scope; a Scope above it is
	// rejected with ErrScopeCeiling. The zero value skips the check (used by
	// the CLI and by an open server that has no principal).
	Ceiling Scope
	// Now is the creation time.
	Now time.Time
}

// CreatedKey is the result of CreateManagedKey.
type CreatedKey struct {
	// Key is the stored record (Digest populated; never expose it).
	Key Key
	// Token is the plaintext credential. It exists only here, once.
	Token string
	// AuditErr is non-nil when the key was created but its audit event could
	// not be recorded. The key is live, so the caller must still deliver Token
	// and should report this.
	AuditErr error
}

// CreateManagedKey validates n, generates a token, stores only its digest in
// ks and records a key.create audit event. It is the one place keys are
// minted, shared by the CLI, the admin API and the dashboard.
func CreateManagedKey(ctx context.Context, ks KeyStore, n NewKey) (CreatedKey, error) {
	name := strings.TrimSpace(n.Name)
	switch {
	case name == "":
		return CreatedKey{}, fmt.Errorf("%w: name is required", ErrInvalidKey)
	case len(name) > MaxKeyNameLen:
		return CreatedKey{}, fmt.Errorf("%w: name is longer than %d bytes", ErrInvalidKey, MaxKeyNameLen)
	case !n.Scope.Valid():
		return CreatedKey{}, fmt.Errorf("%w: scope must be eval, read, write or admin", ErrInvalidKey)
	case !n.ExpiresAt.IsZero() && !n.ExpiresAt.After(n.Now):
		return CreatedKey{}, fmt.Errorf("%w: expiry must be in the future", ErrInvalidKey)
	case n.Ceiling.Valid() && n.Scope > n.Ceiling:
		return CreatedKey{}, ErrScopeCeiling
	}
	tok, err := GenerateToken()
	if err != nil {
		return CreatedKey{}, err
	}
	id, err := NewID()
	if err != nil {
		return CreatedKey{}, err
	}
	evID, err := NewID()
	if err != nil {
		return CreatedKey{}, err
	}
	k := Key{ID: id, Name: name, Digest: sha256.Sum256([]byte(tok)), Scope: n.Scope, CreatedAt: n.Now, ExpiresAt: n.ExpiresAt}
	if err := ks.CreateKey(ctx, k); err != nil {
		return CreatedKey{}, err
	}
	auditErr := ks.AppendAudit(ctx, Event{ID: evID, At: n.Now, Principal: n.Principal, Action: ActionKeyCreate, Target: id, RequestID: n.RequestID})
	return CreatedKey{Key: k, Token: tok, AuditErr: auditErr}, nil
}

// RevokeManagedKey revokes the key with the given id and records a key.revoke
// audit event. err is a KeyStore error (ErrNotFound, ErrAlreadyRevoked) or a
// store failure. auditErr is non-nil when the key was revoked but the audit
// event could not be recorded.
func RevokeManagedKey(ctx context.Context, ks KeyStore, id, principal, requestID string, now time.Time) (auditErr, err error) {
	if err := ks.RevokeKey(ctx, id); err != nil {
		return nil, err
	}
	evID, err := NewID()
	if err != nil {
		return err, nil
	}
	return ks.AppendAudit(ctx, Event{ID: evID, At: now, Principal: principal, Action: ActionKeyRevoke, Target: id, RequestID: requestID}), nil
}
