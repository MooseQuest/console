package server

import (
	"context"
	"crypto/subtle"
	"net/http"

	"github.com/moosequest/console/internal/obs"
)

// CSRF protection (docs/design/auth.md section 4.4). It applies only to
// cookie-authenticated unsafe requests: a session carries a random token that
// the dashboard echoes as X-CSRF-Token (htmx) or the csrf_token form field, and
// http.CrossOriginProtection adds Origin/Sec-Fetch-Site defence in depth.
// Bearer-authenticated requests have no ambient credential and are exempt.

const (
	csrfHeader = "X-CSRF-Token"
	csrfField  = "csrf_token"

	csrfReasonCrossOrigin = "cross_origin"
	csrfReasonToken       = "bad_token"
)

type csrfKey struct{}

// csrfFrom returns the session CSRF token stored in ctx by the auth
// middleware, or "" for bearer, exempt or auth-disabled requests.
func csrfFrom(ctx context.Context) string {
	v, _ := ctx.Value(csrfKey{}).(string)
	return v
}

var crossOrigin = http.NewCrossOriginProtection()

// csrfCheck returns "" when r passes, or a bounded failure reason. It never
// returns or logs the submitted token.
func (s *Server) csrfCheck(r *http.Request, want string) string {
	if err := crossOrigin.Check(r); err != nil {
		return csrfReasonCrossOrigin
	}
	got := r.Header.Get(csrfHeader)
	if got == "" {
		got = r.PostFormValue(csrfField)
	}
	if want == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return csrfReasonToken
	}
	return ""
}

// csrfDeny counts and logs the failure (reason and route only) and answers 403
// with a generic message.
func (s *Server) csrfDeny(w http.ResponseWriter, r *http.Request, pattern, reason string) {
	rec := s.app.Obs
	if rec == nil {
		rec = obs.Nop
	}
	rec.Counter("console_csrf_failures_total", obs.L("reason", reason)).Inc()
	s.log.Warn("csrf check failed", "reason", reason, "method", r.Method, "route", pattern,
		"request_id", RequestID(r.Context()))
	writeJSON(w, http.StatusForbidden, errBody{Error: "forbidden"})
}
