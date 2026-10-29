package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/obs"
)

// scopeRule is the authorization requirement for one mux pattern.
type scopeRule struct {
	// exempt routes are served without authentication.
	exempt bool
	// scope is the minimum scope required when not exempt.
	scope auth.Scope
}

var exempt = scopeRule{exempt: true}

func need(s auth.Scope) scopeRule { return scopeRule{scope: s} }

// routeScopes is the single route-to-scope table, keyed by mux pattern (see
// docs/design/auth.md section 3.2). A matched pattern with no entry is denied
// when auth is enabled, and TestRouteScopesCoverMux fails the build for it, so
// a new route cannot be exposed silently. Entries for /metrics (registered
// only when metrics are on) and the login routes (#75) may precede their
// registration.
var routeScopes = map[string]scopeRule{
	// Unauthenticated always.
	"GET /healthz": exempt,
	"GET /static/": exempt,
	"GET /login":   exempt,
	"POST /login":  exempt,

	// eval.
	"POST /api/flags/{key}/evaluate": need(auth.Eval),

	// read.
	"GET /api/health":           need(auth.Read),
	"GET /api/flags":            need(auth.Read),
	"GET /api/flags/{key}":      need(auth.Read),
	"GET /api/components":       need(auth.Read),
	"GET /api/components/{key}": need(auth.Read),
	"GET /{$}":                  need(auth.Read),
	"GET /flags":                need(auth.Read),
	"GET /status":               need(auth.Read),
	"GET /metrics":              need(auth.Read),

	// write.
	"POST /api/flags":                  need(auth.Write),
	"PUT /api/flags/{key}":             need(auth.Write),
	"DELETE /api/flags/{key}":          need(auth.Write),
	"POST /api/components":             need(auth.Write),
	"PUT /api/components/{key}":        need(auth.Write),
	"DELETE /api/components/{key}":     need(auth.Write),
	"POST /api/components/{key}/check": need(auth.Write),
	"POST /flags/{key}/toggle":         need(auth.Write),
	"POST /status/{key}/check":         need(auth.Write),
}

type principalKey struct{}

// PrincipalFrom returns the authenticated Principal stored in ctx by the auth
// middleware. ok is false when auth is disabled or the route is exempt.
func PrincipalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(auth.Principal)
	return p, ok
}

// WithAuthenticator enables authentication and scope enforcement using a and
// returns s. A nil a (the default) leaves the middleware an inert pass-through
// so behaviour is unchanged until configuration supplies an Authenticator.
// Call before Handler.
func (s *Server) WithAuthenticator(a auth.Authenticator) *Server {
	s.authn = a
	return s
}

// Bounded reasons for console_auth_failures_total; never token-derived.
const (
	reasonUnauthenticated = "unauthenticated"
	reasonForbidden       = "forbidden"
	reasonUnknownRoute    = "unknown_route"
)

// authMiddleware enforces routeScopes in front of the mux. The pattern is
// resolved with mux.Handler (the mux itself only sets r.Pattern while serving).
// Unmatched requests (404/405) pass through to the mux unchanged.
func (s *Server) authMiddleware(next *http.ServeMux) http.Handler {
	if s.authn == nil {
		return next
	}
	rec := s.app.Obs
	if rec == nil {
		rec = obs.Nop
	}
	failures := map[string]obs.Counter{}
	for _, reason := range []string{reasonUnauthenticated, reasonForbidden, reasonUnknownRoute} {
		failures[reason] = rec.Counter("console_auth_failures_total", obs.L("reason", reason))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := next.Handler(r)
		if pattern == "" {
			next.ServeHTTP(w, r)
			return
		}
		rule, known := routeScopes[pattern]
		if known && rule.exempt {
			next.ServeHTTP(w, r)
			return
		}
		mutating := isMutating(r.Method)
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		p, err := s.authn.Authenticate(r)
		switch {
		case err != nil:
			if !errors.Is(err, auth.ErrUnauthenticated) {
				s.log.Error("authenticator error", "request_id", RequestID(r.Context()))
			}
			failures[reasonUnauthenticated].Inc()
			sr.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(sr, http.StatusUnauthorized, errBody{Error: "unauthorized"})
			s.audit(r, pattern, "-", 0, sr.status, mutating)
			return
		case !known:
			failures[reasonUnknownRoute].Inc()
			writeJSON(sr, http.StatusForbidden, errBody{Error: "forbidden"})
			s.audit(r, pattern, p.Name, p.Scope, sr.status, mutating)
			return
		case !p.Scope.Allows(rule.scope):
			failures[reasonForbidden].Inc()
			writeJSON(sr, http.StatusForbidden, errBody{Error: "forbidden"})
			s.audit(r, pattern, p.Name, p.Scope, sr.status, mutating)
			return
		}

		r2 := r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
		// The mux records the matched pattern on r2; propagate it back so
		// outer middleware (logging, metrics) see it.
		defer func() { r.Pattern = r2.Pattern }()
		next.ServeHTTP(sr, r2)
		s.audit(r, pattern, p.Name, p.Scope, sr.status, mutating)
	})
}

// isMutating reports whether m is a state-changing method.
func isMutating(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// audit logs one line per mutating request: principal name, scope, method,
// route pattern, status and request ID. It never logs credentials, bodies or
// config values.
func (s *Server) audit(r *http.Request, pattern, name string, sc auth.Scope, status int, mutating bool) {
	if !mutating {
		return
	}
	scope := "-"
	if sc.Valid() {
		scope = sc.String()
	}
	s.log.Info("audit",
		"principal", name,
		"scope", scope,
		"method", r.Method,
		"route", pattern,
		"status", status,
		"request_id", RequestID(r.Context()))
}
