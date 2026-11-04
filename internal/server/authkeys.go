package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moosequest/console/internal/auth"
)

// Admin API for managed API keys and their audit trail (docs/design/auth.md
// section 3.2). All four routes need the admin scope and are registered
// whether or not a KeyStore exists; without one they answer 501. Key creation
// and revocation go through auth.CreateManagedKey / auth.RevokeManagedKey, the
// same engine the CLI uses.

const (
	// maxKeyBodyBytes caps the create-key request body.
	maxKeyBodyBytes = 4 << 10 // 4 KiB
	// defaultAuditLimit and maxAuditLimit bound GET /api/auth/audit.
	defaultAuditLimit = 100
	maxAuditLimit     = 1000
	// anonymousPrincipal is the audit label when auth is disabled and there is
	// no principal.
	anonymousPrincipal = "anonymous"
)

// keyView is the public shape of a managed key. It never carries the token or
// its digest.
type keyView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Scope      string  `json:"scope"`
	Status     string  `json:"status"`
	CreatedAt  *string `json:"created_at"`
	ExpiresAt  *string `json:"expires_at"`
	LastUsedAt *string `json:"last_used_at"`
	RevokedAt  *string `json:"revoked_at"`
}

// createdKeyView is keyView plus the plaintext token, returned exactly once.
type createdKeyView struct {
	keyView
	Key string `json:"key"`
}

// auditView is the public shape of an audit event.
type auditView struct {
	ID        string  `json:"id"`
	At        *string `json:"at"`
	Principal string  `json:"principal"`
	Action    string  `json:"action"`
	Target    string  `json:"target"`
	RequestID string  `json:"request_id"`
}

func viewTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func newKeyView(k auth.Key, now time.Time) keyView {
	status := "active"
	switch {
	case k.Revoked():
		status = "revoked"
	case k.Expired(now):
		status = "expired"
	}
	return keyView{
		ID: k.ID, Name: k.Name, Scope: k.Scope.String(), Status: status,
		CreatedAt: viewTime(k.CreatedAt), ExpiresAt: viewTime(k.ExpiresAt),
		LastUsedAt: viewTime(k.LastUsedAt), RevokedAt: viewTime(k.RevokedAt),
	}
}

// keyStore returns the configured KeyStore, or writes 501 and returns nil.
// Responses on these routes are never cacheable.
func (s *Server) keyStore(w http.ResponseWriter) auth.KeyStore {
	w.Header().Set("Cache-Control", "no-store")
	if s.app.Keys == nil {
		writeJSON(w, http.StatusNotImplemented, errBody{
			Error: "managed keys are unavailable: this storage backend has no key store"})
		return nil
	}
	return s.app.Keys
}

// internalError logs err (never request contents) and answers a generic 500.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, what string, err error) {
	s.log.Error(what, "err", err, "request_id", RequestID(r.Context()))
	writeJSON(w, http.StatusInternalServerError, errBody{Error: "internal error"})
}

// actor returns the calling principal. With auth disabled there is none, so
// the audit label is "anonymous" and ok is false.
func actor(r *http.Request) (p auth.Principal, name string, ok bool) {
	p, ok = PrincipalFrom(r.Context())
	if !ok {
		return p, anonymousPrincipal, false
	}
	return p, p.Name, true
}

// createKeyRequest is the POST /api/auth/keys body.
type createKeyRequest struct {
	Name      string  `json:"name"`
	Scope     string  `json:"scope"`
	ExpiresAt *string `json:"expires_at"`
}

// handleCreateKey creates a managed key and returns it with 201, including the
// plaintext token, which is never shown again.
func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	ks := s.keyStore(w)
	if ks == nil {
		return
	}
	var req createKeyRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxKeyBodyBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(&req)
	if err == nil {
		// Reject trailing content after the object.
		if _, terr := dec.Token(); terr != io.EOF {
			err = errors.New("trailing data")
		}
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errBody{Error: "request body too large"})
			return
		}
		badRequest(w, "invalid key JSON")
		return
	}
	scope, err := auth.ParseScope(req.Scope)
	if err != nil {
		badRequest(w, "scope must be eval, read, write or admin")
		return
	}
	now := time.Now().UTC()
	var expires time.Time
	if req.ExpiresAt != nil {
		expires, err = time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			badRequest(w, "expires_at must be an RFC3339 timestamp")
			return
		}
		expires = expires.UTC()
		// An explicit expiry must be a real future instant. The zero time
		// (0001-01-01) would otherwise read as "no expiry" downstream and
		// silently mint a key that never expires.
		if !expires.After(now) {
			badRequest(w, "expiry must be in the future")
			return
		}
	}
	p, name, authed := actor(r)
	n := auth.NewKey{
		Name: req.Name, Scope: scope, ExpiresAt: expires,
		Principal: name, RequestID: RequestID(r.Context()), Now: now,
	}
	if authed {
		n.Ceiling = p.Scope
	}
	res, err := auth.CreateManagedKey(r.Context(), ks, n)
	switch {
	case errors.Is(err, auth.ErrInvalidKey):
		badRequest(w, strings.TrimPrefix(err.Error(), auth.ErrInvalidKey.Error()+": "))
		return
	case errors.Is(err, auth.ErrScopeCeiling):
		writeJSON(w, http.StatusForbidden, errBody{Error: "requested scope exceeds your own scope"})
		return
	case errors.Is(err, auth.ErrDuplicateName):
		writeJSON(w, http.StatusConflict, errBody{Error: "a key with that name already exists"})
		return
	case err != nil:
		s.internalError(w, r, "create key failed", err)
		return
	}
	if res.AuditErr != nil {
		// The key is live; deliver the token anyway and surface the failure
		// to operators.
		s.log.Error("key created but audit write failed", "err", res.AuditErr, "request_id", RequestID(r.Context()))
	}
	writeJSON(w, http.StatusCreated, createdKeyView{keyView: newKeyView(res.Key, now), Key: res.Token})
}

// handleListKeys returns every managed key (never digests or tokens).
func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	ks := s.keyStore(w)
	if ks == nil {
		return
	}
	keys, err := ks.ListKeys(r.Context())
	if err != nil {
		s.internalError(w, r, "list keys failed", err)
		return
	}
	now := time.Now().UTC()
	out := make([]keyView, 0, len(keys))
	for _, k := range keys {
		out = append(out, newKeyView(k, now))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRevokeKey revokes a key by id and returns 204. Revoking the key that
// authenticated the request (directly or through a dashboard session) is a 409
// unless ?force=true.
func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	ks := s.keyStore(w)
	if ks == nil {
		return
	}
	id := r.PathValue("id")
	p, name, authed := actor(r)
	if authed && p.KeyID != "" && p.KeyID == id && !forceRequested(r) {
		writeJSON(w, http.StatusConflict, errBody{
			Error: "this is the key authenticating the request; pass ?force=true to revoke it anyway"})
		return
	}
	auditErr, err := auth.RevokeManagedKey(r.Context(), ks, id, name, RequestID(r.Context()), time.Now().UTC())
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errBody{Error: "key not found"})
		return
	case errors.Is(err, auth.ErrAlreadyRevoked):
		writeJSON(w, http.StatusConflict, errBody{Error: "key already revoked"})
		return
	case err != nil:
		s.internalError(w, r, "revoke key failed", err)
		return
	}
	if auditErr != nil {
		s.log.Error("key revoked but audit write failed", "err", auditErr, "request_id", RequestID(r.Context()))
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListAudit returns key-management audit events, newest first. since
// (RFC3339) is floored at now minus the retention window so rows awaiting the
// daily prune are never shown.
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	ks := s.keyStore(w)
	if ks == nil {
		return
	}
	q := r.URL.Query()
	f := auth.AuditFilter{Limit: defaultAuditLimit}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxAuditLimit {
			badRequest(w, "limit must be an integer from 1 to "+strconv.Itoa(maxAuditLimit))
			return
		}
		f.Limit = n
	}
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			badRequest(w, "since must be an RFC3339 timestamp")
			return
		}
		f.Since = t.UTC()
	}
	if ret := s.app.Config.AuditRetention(); ret > 0 {
		if floor := time.Now().UTC().Add(-ret); f.Since.Before(floor) {
			f.Since = floor
		}
	}
	evs, err := ks.ListAudit(r.Context(), f)
	if err != nil {
		s.internalError(w, r, "list audit failed", err)
		return
	}
	out := make([]auditView, 0, len(evs))
	for _, e := range evs {
		out = append(out, auditView{ID: e.ID, At: viewTime(e.At), Principal: e.Principal,
			Action: e.Action, Target: e.Target, RequestID: e.RequestID})
	}
	writeJSON(w, http.StatusOK, out)
}

// forceRequested reports whether the caller passed exactly one force
// parameter whose value is exactly "true". Duplicate or differently cased
// values never count, so the override cannot depend on parameter order.
func forceRequested(r *http.Request) bool {
	v := r.URL.Query()["force"]
	return len(v) == 1 && v[0] == "true"
}
