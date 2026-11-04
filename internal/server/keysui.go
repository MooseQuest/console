package server

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/moosequest/console/internal/auth"
)

// Dashboard page for managed API keys (docs/design/keys-ui.md). Mutations are
// plain form posts (htmx does not swap 4xx responses) that go through the same
// auth.CreateManagedKey / auth.RevokeManagedKey engine as the admin API, so
// scope ceiling, duplicate names, expiry rules and persisted audit events are
// identical. The token appears only in the body of the create response.

const (
	timeLayout = "2006-01-02 15:04 UTC"
	// defaultPreset is the audit window shown when none is requested.
	defaultPreset = "24h"
)

// auditPresets are the accepted ?since= values, in display order. A zero
// duration means "all retained".
var auditPresets = []struct {
	value, label string
	d            time.Duration
}{
	{"1h", "1 hour", time.Hour},
	{"24h", "24 hours", 24 * time.Hour},
	{"7d", "7 days", 7 * 24 * time.Hour},
	{"all", "All retained", 0},
}

// keyRow is one table row of the key list. It never carries a token or digest.
type keyRow struct {
	ID, Name, Scope       string
	Created, CreatedISO   string
	Expires, ExpiresISO   string
	LastUsed, LastUsedISO string
	Status, Dot           string
	Active                bool
}

type createdNote struct{ Name, Scope, Token string }

type keyForm struct{ Name, Scope, Expires string }

type confirmData struct {
	ID, Name string
	Self     bool
}

type presetLink struct {
	Value, Label string
	Current      bool
}

type auditRow struct {
	At, AtISO, Principal, Action, Key, RequestID string
}

// auditData drives the swappable audit fragment.
type auditData struct {
	Presets   []presetLink
	Events    []auditRow
	Retention string
	Limited   bool
	Limit     int
	Err       bool
}

// keysData drives every response of the keys page. Mode selects the body:
// "list" (default), "confirm", "message" or "unavailable".
type keysData struct {
	pageData
	Mode        string
	Keys        []keyRow
	ListErr     bool
	Revoked     bool
	AuditNotice bool
	Created     *createdNote
	Form        keyForm
	Error       string
	ErrorField  string
	Scopes      []string
	MaxName     int
	Audit       auditData
	Confirm     confirmData
	Heading     string
	Message     string
	Alert       bool
	RequestID   string
}

// keysShell builds the common page data for a keys response.
func (s *Server) keysShell(r *http.Request, mode string) keysData {
	pd := s.page(r, "API keys", "keys")
	pd.NoHistory = true
	return keysData{pageData: pd, Mode: mode, MaxName: auth.MaxKeyNameLen}
}

// renderKeys writes a keys page with the given status. Every keys response is
// uncacheable.
func (s *Server) renderKeys(w http.ResponseWriter, status int, d keysData) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, "keys", "keys.html", d)
}

// keysMessage renders a small message page (404, 409, 400, 500, ...).
func (s *Server) keysMessage(w http.ResponseWriter, r *http.Request, status int, heading, msg string) {
	d := s.keysShell(r, "message")
	d.Heading, d.Message, d.Alert = heading, msg, status >= 400
	if status >= 500 {
		d.RequestID = RequestID(r.Context())
	}
	s.renderKeys(w, status, d)
}

// keysInternal logs err (never request contents) and answers a generic 500
// page carrying the request ID.
func (s *Server) keysInternal(w http.ResponseWriter, r *http.Request, what string, err error) {
	s.log.Error(what, "err", err, "request_id", RequestID(r.Context()))
	s.keysMessage(w, r, http.StatusInternalServerError, "Something went wrong", "The request could not be completed.")
}

// keysStore returns the KeyStore or answers the 501 page and returns nil.
func (s *Server) keysStore(w http.ResponseWriter, r *http.Request) auth.KeyStore {
	if s.app.Keys == nil {
		s.renderKeys(w, http.StatusNotImplemented, s.keysShell(r, "unavailable"))
		return nil
	}
	return s.app.Keys
}

// callerScope is the scope used for the create form's ceiling. With auth
// disabled there is no principal and no ceiling.
func callerScope(r *http.Request) auth.Scope {
	if p, ok := PrincipalFrom(r.Context()); ok {
		return p.Scope
	}
	return auth.Admin
}

func scopeOptions(ceiling auth.Scope) []string {
	var out []string
	for _, sc := range []auth.Scope{auth.Eval, auth.Read, auth.Write, auth.Admin} {
		if ceiling.Allows(sc) {
			out = append(out, sc.String())
		}
	}
	return out
}

func fmtTime(t time.Time) (human, iso string) {
	if t.IsZero() {
		return "", ""
	}
	t = t.UTC()
	return t.Format(timeLayout), t.Format(time.RFC3339)
}

// keyRows converts keys for display: active first, then newest first.
func keyRows(keys []auth.Key, now time.Time) []keyRow {
	rows := make([]keyRow, 0, len(keys))
	for _, k := range keys {
		v := newKeyView(k, now)
		row := keyRow{ID: k.ID, Name: k.Name, Scope: v.Scope, Active: v.Status == "active"}
		row.Created, row.CreatedISO = fmtTime(k.CreatedAt)
		row.Expires, row.ExpiresISO = fmtTime(k.ExpiresAt)
		row.LastUsed, row.LastUsedISO = fmtTime(k.LastUsedAt)
		switch v.Status {
		case "active":
			row.Status, row.Dot = "Active", "operational"
		case "expired":
			row.Status, row.Dot = "Expired", "degraded"
		default:
			row.Status, row.Dot = "Revoked", "down"
		}
		rows = append(rows, row)
	}
	created := map[string]time.Time{}
	for _, k := range keys {
		created[k.ID] = k.CreatedAt
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Active != rows[j].Active {
			return rows[i].Active
		}
		return created[rows[i].ID].After(created[rows[j].ID])
	})
	return rows
}

// retentionLine states the effective audit retention from config.
func (s *Server) retentionLine() string {
	ret := s.app.Config.AuditRetention()
	if ret <= 0 {
		return "Audit events are kept forever."
	}
	return fmt.Sprintf("Audit events are kept for %d days.", int(ret/(24*time.Hour)))
}

// parsePreset validates a ?since= preset. Empty means the default.
func parsePreset(v string) (value string, d time.Duration, ok bool) {
	if v == "" {
		v = defaultPreset
	}
	for _, p := range auditPresets {
		if p.value == v {
			return p.value, p.d, true
		}
	}
	return "", 0, false
}

// auditFor loads the audit view for a validated preset. names maps key ids to
// names so rows show a name when the key still exists.
func (s *Server) auditFor(r *http.Request, ks auth.KeyStore, preset string, d time.Duration, names map[string]string) auditData {
	out := auditData{Retention: s.retentionLine(), Limit: defaultAuditLimit}
	for _, p := range auditPresets {
		out.Presets = append(out.Presets, presetLink{Value: p.value, Label: p.label, Current: p.value == preset})
	}
	f := auth.AuditFilter{Limit: defaultAuditLimit}
	if d > 0 {
		f.Since = time.Now().UTC().Add(-d)
	}
	evs, err := ks.ListAudit(r.Context(), s.floorAudit(f))
	if err != nil {
		s.log.Error("list audit failed", "err", err, "request_id", RequestID(r.Context()))
		out.Err = true
		return out
	}
	for _, e := range evs {
		at, iso := fmtTime(e.At)
		key := e.Target
		if n, ok := names[e.Target]; ok {
			key = n
		}
		out.Events = append(out.Events, auditRow{At: at, AtISO: iso, Principal: e.Principal,
			Action: e.Action, Key: key, RequestID: e.RequestID})
	}
	out.Limited = len(evs) >= defaultAuditLimit
	return out
}

// listPage fills d with the key list and audit view and renders it with status.
// since is an already validated preset.
func (s *Server) listPage(w http.ResponseWriter, r *http.Request, ks auth.KeyStore, status int, d keysData, since string) {
	preset, dur, _ := parsePreset(since)
	d.Scopes = scopeOptions(callerScope(r))
	if d.Form.Scope == "" {
		d.Form.Scope = auth.Read.String()
	}
	keys, err := ks.ListKeys(r.Context())
	names := map[string]string{}
	if err != nil {
		// Not fatal for a just-created key: the secret must still be shown.
		s.log.Error("list keys failed", "err", err, "request_id", RequestID(r.Context()))
		d.ListErr = true
	} else {
		d.Keys = keyRows(keys, time.Now().UTC())
		for _, k := range keys {
			names[k.ID] = k.Name
		}
	}
	d.Audit = s.auditFor(r, ks, preset, dur, names)
	s.renderKeys(w, status, d)
}

// handleKeysPage renders the keys page: create form, key list and audit log.
func (s *Server) handleKeysPage(w http.ResponseWriter, r *http.Request) {
	ks := s.keysStore(w, r)
	if ks == nil {
		return
	}
	q := r.URL.Query()
	preset, _, ok := parsePreset(q.Get("since"))
	if !ok {
		s.keysMessage(w, r, http.StatusBadRequest, "Invalid request", "since must be one of 1h, 24h, 7d or all.")
		return
	}
	d := s.keysShell(r, "list")
	d.Revoked = q.Get("revoked") == "1"
	s.listPage(w, r, ks, http.StatusOK, d, preset)
}

// handleKeysAudit renders only the audit fragment for htmx swaps.
func (s *Server) handleKeysAudit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.app.Keys == nil {
		s.renderKeys(w, http.StatusNotImplemented, s.keysShell(r, "unavailable"))
		return
	}
	preset, dur, ok := parsePreset(r.URL.Query().Get("since"))
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<p class="login-error" role="alert">since must be one of 1h, 24h, 7d or all.</p>`))
		return
	}
	names := map[string]string{}
	if keys, err := s.app.Keys.ListKeys(r.Context()); err == nil {
		for _, k := range keys {
			names[k.ID] = k.Name
		}
	}
	d := s.auditFor(r, s.app.Keys, preset, dur, names)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.renderPartial(w, "keys-audit", "keys.html", d)
}

// readForm caps and parses the request body. It reports false after writing a
// 413 or 400 page. The auth middleware may already have parsed the form for the
// CSRF check; the explicit length checks keep the 4 KiB cap regardless.
func (s *Server) readForm(w http.ResponseWriter, r *http.Request) bool {
	tooBig := func() bool {
		s.keysMessage(w, r, http.StatusRequestEntityTooLarge, "Request too large", "The request body is too large.")
		return false
	}
	if r.ContentLength > maxKeyBodyBytes {
		return tooBig()
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxKeyBodyBytes)
	if err := r.ParseForm(); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return tooBig()
		}
		s.keysMessage(w, r, http.StatusBadRequest, "Invalid request", "The form could not be read.")
		return false
	}
	n := 0
	for k, vs := range r.PostForm {
		n += len(k)
		for _, v := range vs {
			n += len(v)
		}
	}
	if n > maxKeyBodyBytes {
		return tooBig()
	}
	return true
}

// handleKeysCreate creates a key from the form and shows the token once.
func (s *Server) handleKeysCreate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ks := s.keysStore(w, r)
	if ks == nil {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	form := keyForm{
		Name:    strings.TrimSpace(r.PostForm.Get("name")),
		Scope:   strings.ToLower(strings.TrimSpace(r.PostForm.Get("scope"))),
		Expires: strings.TrimSpace(r.PostForm.Get("expires")),
	}
	fail := func(status int, field, msg string) {
		d := s.keysShell(r, "list")
		d.Form, d.Error, d.ErrorField = form, msg, field
		s.listPage(w, r, ks, status, d, defaultPreset)
	}

	scope, err := auth.ParseScope(form.Scope)
	if err != nil {
		fail(http.StatusBadRequest, "scope", "Scope must be eval, read, write or admin.")
		return
	}
	now := time.Now().UTC()
	var expires time.Time
	if form.Expires != "" {
		day, err := time.Parse("2006-01-02", form.Expires)
		if err != nil {
			fail(http.StatusBadRequest, "expires", "Expiry must be a date.")
			return
		}
		// End of that day, UTC.
		expires = day.UTC().Add(24*time.Hour - time.Second)
	}
	p, name, authed := actor(r)
	n := auth.NewKey{
		Name: form.Name, Scope: scope, ExpiresAt: expires,
		Principal: name, RequestID: RequestID(r.Context()), Now: now,
	}
	if authed {
		n.Ceiling = p.Scope
	}
	res, err := auth.CreateManagedKey(r.Context(), ks, n)
	switch {
	case errors.Is(err, auth.ErrInvalidKey):
		msg := strings.TrimPrefix(err.Error(), auth.ErrInvalidKey.Error()+": ")
		field := "name"
		switch {
		case strings.HasPrefix(msg, "scope"):
			field = "scope"
		case strings.HasPrefix(msg, "expiry"):
			field = "expires"
		}
		fail(http.StatusBadRequest, field, strings.ToUpper(msg[:1])+msg[1:]+".")
		return
	case errors.Is(err, auth.ErrScopeCeiling):
		fail(http.StatusForbidden, "scope", "You cannot create a key with a higher scope than your own.")
		return
	case errors.Is(err, auth.ErrDuplicateName):
		fail(http.StatusConflict, "name", "A key with that name already exists.")
		return
	case err != nil:
		s.keysInternal(w, r, "create key failed", err)
		return
	}
	if res.AuditErr != nil {
		s.log.Error("key created but audit write failed", "err", res.AuditErr, "request_id", RequestID(r.Context()))
	}
	// The secret must not be cached, snapshotted by htmx history or linked to
	// by any URL: it exists only in this response body.
	w.Header().Set("HX-Push-Url", "false")
	d := s.keysShell(r, "list")
	d.Created = &createdNote{Name: res.Key.Name, Scope: res.Key.Scope.String(), Token: res.Token}
	d.AuditNotice = res.AuditErr != nil
	s.listPage(w, r, ks, http.StatusCreated, d, defaultPreset)
}

// handleKeysRevoke is the two-step revoke: without confirm=1 it shows a
// confirmation page and changes nothing; with it, it revokes.
func (s *Server) handleKeysRevoke(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ks := s.keysStore(w, r)
	if ks == nil {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	id := r.PathValue("id")
	keys, err := ks.ListKeys(r.Context())
	if err != nil {
		s.keysInternal(w, r, "list keys failed", err)
		return
	}
	var target *auth.Key
	for i := range keys {
		if keys[i].ID == id {
			target = &keys[i]
		}
	}
	if target == nil {
		s.keysMessage(w, r, http.StatusNotFound, "Key not found", "That key does not exist.")
		return
	}
	if target.Revoked() {
		s.keysMessage(w, r, http.StatusConflict, "Already revoked", "That key is already revoked.")
		return
	}
	// Self-revoke is decided here from the authenticated principal, never from
	// a form field.
	p, name, authed := actor(r)
	self := authed && p.KeyID != "" && p.KeyID == id

	if r.PostForm.Get("confirm") != "1" {
		d := s.keysShell(r, "confirm")
		d.Confirm = confirmData{ID: target.ID, Name: target.Name, Self: self}
		s.renderKeys(w, http.StatusOK, d)
		return
	}

	auditErr, err := auth.RevokeManagedKey(r.Context(), ks, id, name, RequestID(r.Context()), time.Now().UTC())
	switch {
	case errors.Is(err, auth.ErrNotFound):
		s.keysMessage(w, r, http.StatusNotFound, "Key not found", "That key does not exist.")
		return
	case errors.Is(err, auth.ErrAlreadyRevoked):
		s.keysMessage(w, r, http.StatusConflict, "Already revoked", "That key is already revoked.")
		return
	case err != nil:
		s.keysInternal(w, r, "revoke key failed", err)
		return
	}
	if auditErr != nil {
		s.log.Error("key revoked but audit write failed", "err", auditErr, "request_id", RequestID(r.Context()))
	}
	if self {
		// End the session now, as logout does; the Chain digest re-check would
		// reject it on the next request anyway.
		if c, err := r.Cookie(sessionCookie); err == nil {
			s.sessions.Delete(c.Value)
		}
		http.SetCookie(w, s.cookie(r, "", -1))
		http.Redirect(w, r, "/login?signed_out=1&key_revoked=1", http.StatusSeeOther)
		return
	}
	if auditErr != nil {
		d := s.keysShell(r, "list")
		d.Revoked, d.AuditNotice = true, true
		s.listPage(w, r, ks, http.StatusOK, d, defaultPreset)
		return
	}
	http.Redirect(w, r, "/keys?revoked=1", http.StatusSeeOther)
}
