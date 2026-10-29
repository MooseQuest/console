package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

// Session lifetimes (docs/design/auth.md section 4.4).
const (
	// SessionIdleTimeout is how long a session survives without a request.
	SessionIdleTimeout = 12 * time.Hour
	// SessionMaxLifetime is the absolute lifetime of a session.
	SessionMaxLifetime = 7 * 24 * time.Hour
	// MaxSessions bounds the in-memory session table.
	MaxSessions = 10000
)

// Session is a server-side dashboard login. It holds the digest of the token
// that was presented at login, never the token itself.
type Session struct {
	Principal Principal
	// Digest is SHA-256 of the login token; re-checked against the current
	// token set on every request so removing a token revokes its sessions.
	Digest [sha256.Size]byte
	// CSRF is the per-session synchronizer token (256 random bits,
	// base64url). It is delivered to templates and compared in constant time
	// on cookie-authenticated unsafe requests; it is never logged.
	CSRF    string
	Created time.Time
	Seen    time.Time
}

// SessionStore is an in-memory, bounded session table. A restart logs everyone
// out. It is safe for concurrent use.
type SessionStore struct {
	mu       sync.Mutex
	now      func() time.Time
	max      int
	sessions map[string]*Session
}

// NewSessionStore returns an empty store. now may be nil (time.Now); tests
// inject a fake clock.
func NewSessionStore(now func() time.Time) *SessionStore {
	if now == nil {
		now = time.Now
	}
	return &SessionStore{now: now, max: MaxSessions, sessions: map[string]*Session{}}
}

// newID returns 256 random bits, base64url-encoded.
func newID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func expired(v *Session, now time.Time) bool {
	return now.Sub(v.Seen) > SessionIdleTimeout || now.Sub(v.Created) > SessionMaxLifetime
}

// Create stores a new session for p and returns its ID. When the table is
// full, expired sessions are purged first, then the least recently used one is
// evicted.
func (s *SessionStore) Create(p Principal, digest [sha256.Size]byte) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	csrf, err := newID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if len(s.sessions) >= s.max {
		for k, v := range s.sessions {
			if expired(v, now) {
				delete(s.sessions, k)
			}
		}
	}
	if len(s.sessions) >= s.max {
		var oldest string
		var ot time.Time
		for k, v := range s.sessions {
			if oldest == "" || v.Seen.Before(ot) {
				oldest, ot = k, v.Seen
			}
		}
		delete(s.sessions, oldest)
	}
	s.sessions[id] = &Session{Principal: p, Digest: digest, CSRF: csrf, Created: now, Seen: now}
	return id, nil
}

// Get returns a copy of the live session for id and refreshes its idle timer.
// Expired sessions are purged and reported as absent.
func (s *SessionStore) Get(id string) (Session, bool) {
	if id == "" {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[id]
	if !ok {
		return Session{}, false
	}
	now := s.now()
	if expired(v, now) {
		delete(s.sessions, id)
		return Session{}, false
	}
	v.Seen = now
	return *v, true
}

// Delete destroys the session (logout, or revocation).
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// Len reports the number of stored sessions, expired ones included until purged.
func (s *SessionStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}
