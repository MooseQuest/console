// Package auth defines Console's API authentication seam: scopes, principals,
// the Authenticator interface, and a static bearer-token implementation.
//
// See docs/design/auth.md. The package depends only on the standard library so
// that internal/core stays dependency-free and every consumer can import it.
// Tokens are never retained in plaintext: only SHA-256 digests are held, and
// no error or Principal ever contains any part of a token.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// ErrUnauthenticated is returned when a request carries no valid credential.
var ErrUnauthenticated = errors.New("auth: unauthenticated")

// Scope is a hierarchical permission level: Eval < Read < Write < Admin.
type Scope int

// The defined scopes, in increasing order of privilege. The zero value is
// deliberately not a valid scope.
const (
	Eval Scope = iota + 1
	Read
	Write
	Admin
)

// String returns the lowercase scope name ("eval", "read", "write", "admin").
func (s Scope) String() string {
	switch s {
	case Eval:
		return "eval"
	case Read:
		return "read"
	case Write:
		return "write"
	case Admin:
		return "admin"
	}
	return "invalid"
}

// Valid reports whether s is one of the defined scopes.
func (s Scope) Valid() bool { return s >= Eval && s <= Admin }

// Allows reports whether s satisfies the required scope. Scopes are
// hierarchical, so Admin allows everything. An invalid scope allows nothing.
func (s Scope) Allows(required Scope) bool {
	return s.Valid() && required.Valid() && s >= required
}

// Principal identifies an authenticated caller.
type Principal struct {
	// Name is an audit label (scope-N plus a digest prefix), never the token.
	Name  string
	Scope Scope
}

// Authenticator turns a request into a Principal or ErrUnauthenticated.
type Authenticator interface {
	Authenticate(r *http.Request) (Principal, error)
}

type entry struct {
	digest [sha256.Size]byte
	p      Principal
}

// Static authenticates bearer tokens configured up front. It retains only
// SHA-256 digests of the tokens.
type Static struct {
	entries []entry
}

// NewStatic builds a Static authenticator from per-scope token lists. Blank
// tokens are ignored. A token configured under several scopes resolves to the
// highest of them. Principal names are "<scope>-<N>-<hex>" where N is the
// 1-based position in that scope's list and hex is the first 6 hex characters
// of the token digest, e.g. "write-2-1a2b3c".
func NewStatic(tokens map[Scope][]string) *Static {
	scopes := make([]Scope, 0, len(tokens))
	for sc := range tokens {
		if sc.Valid() {
			scopes = append(scopes, sc)
		}
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i] < scopes[j] })

	best := map[[sha256.Size]byte]entry{}
	var order [][sha256.Size]byte
	for _, sc := range scopes {
		n := 0
		for _, t := range tokens[sc] {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			n++
			d := sha256.Sum256([]byte(t))
			name := fmt.Sprintf("%s-%d-%s", sc, n, hex.EncodeToString(d[:])[:6])
			if _, seen := best[d]; !seen {
				order = append(order, d)
			}
			// Scopes are visited ascending, so later wins = highest scope.
			best[d] = entry{digest: d, p: Principal{Name: name, Scope: sc}}
		}
	}
	s := &Static{}
	for _, d := range order {
		s.entries = append(s.entries, best[d])
	}
	return s
}

// Authenticate extracts "Authorization: Bearer <token>" and matches it against
// the configured digests in constant time.
func (s *Static) Authenticate(r *http.Request) (Principal, error) {
	h := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return Principal{}, ErrUnauthenticated
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return Principal{}, ErrUnauthenticated
	}
	return s.AuthenticateDigest(sha256.Sum256([]byte(tok)))
}

// DigestAuthenticator re-resolves a principal from a token digest alone. The
// dashboard session store keeps only the digest of the token a user logged in
// with and uses this to confirm on every request that the token is still
// configured.
type DigestAuthenticator interface {
	AuthenticateDigest(d [sha256.Size]byte) (Principal, error)
}

// AuthenticateDigest matches d against the configured digests in constant time.
func (s *Static) AuthenticateDigest(d [sha256.Size]byte) (Principal, error) {
	var found Principal
	ok := false
	// Scan every entry so timing does not reveal which one matched.
	for _, e := range s.entries {
		if subtle.ConstantTimeCompare(d[:], e.digest[:]) == 1 {
			found = e.p
			ok = true
		}
	}
	if !ok {
		return Principal{}, ErrUnauthenticated
	}
	return found, nil
}

// TokenPrefix is the recognisable prefix of generated tokens, so secret
// scanners can match them.
const TokenPrefix = "csk_"

// GenerateToken returns a new token: 256 random bits, base64url (unpadded),
// with the "csk_" prefix.
func GenerateToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}
