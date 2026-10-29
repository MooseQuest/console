// Package server is Console's HTTP layer. It exposes a JSON API over the flag
// and status engines and a small server-rendered dashboard (htmx) on top of the
// same App. It owns no business logic: every handler is a thin adapter that
// decodes a request, calls an engine, and encodes the result.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/web"
)

// Server wires an App to an http.ServeMux and the dashboard templates. Construct
// it with New, register routes with Routes, and obtain the wrapped handler with
// Handler.
type Server struct {
	app  *app.App
	mux  *http.ServeMux
	tmpl *template.Template
	log  *slog.Logger

	// authn authenticates requests; nil disables authorization entirely (the
	// middleware is a pass-through). See WithAuthenticator.
	authn auth.Authenticator
	// patterns records every pattern registered on the mux, so tests can
	// assert each has an entry in routeScopes.
	patterns []string
}

// New builds a Server over a. It parses the embedded dashboard templates and
// registers all routes; the returned Server is ready to serve.
func New(a *app.App) *Server {
	s := &Server{
		app:  a,
		mux:  http.NewServeMux(),
		tmpl: parseTemplates(),
		log:  slog.Default(),
	}
	s.Routes()
	return s
}

// parseTemplates loads the base layout plus every page/partial template from the
// embedded web.Templates FS into a single template set. Each page defines its
// own named entry ("overview"/"flags"/"status"); the shared "content" block is
// supplied per-render via cloned, page-scoped template sets (see render).
func parseTemplates() *template.Template {
	return template.Must(template.ParseFS(web.Templates, "templates/base.html"))
}

// Routes registers every API and UI route on the mux. It is safe to call once
// during construction.
func (s *Server) Routes() {
	// Unauthenticated liveness probe: no body detail, no component inventory.
	s.handleFunc("GET /healthz", s.handleHealthz)

	// JSON API.
	s.handleFunc("GET /api/health", s.handleHealth)

	s.handleFunc("GET /api/flags", s.handleListFlags)
	s.handleFunc("POST /api/flags", s.handleCreateFlag)
	s.handleFunc("GET /api/flags/{key}", s.handleGetFlag)
	s.handleFunc("PUT /api/flags/{key}", s.handleUpdateFlag)
	s.handleFunc("DELETE /api/flags/{key}", s.handleDeleteFlag)
	s.handleFunc("POST /api/flags/{key}/evaluate", s.handleEvaluateFlag)

	s.handleFunc("GET /api/components", s.handleListComponents)
	s.handleFunc("POST /api/components", s.handleCreateComponent)
	s.handleFunc("GET /api/components/{key}", s.handleGetComponent)
	s.handleFunc("PUT /api/components/{key}", s.handleUpdateComponent)
	s.handleFunc("DELETE /api/components/{key}", s.handleDeleteComponent)
	s.handleFunc("POST /api/components/{key}/check", s.handleCheckComponent)

	// Dashboard UI.
	s.handleFunc("GET /{$}", s.handleOverview)
	s.handleFunc("GET /flags", s.handleFlagsPage)
	s.handleFunc("POST /flags/{key}/toggle", s.handleToggleFlag)
	s.handleFunc("GET /status", s.handleStatusPage)
	s.handleFunc("POST /status/{key}/check", s.handleCheckComponentUI)

	// Static assets (CSS, vendored JS).
	static := http.FileServerFS(web.Static)
	s.handle("GET /static/", static)

	// Prometheus metrics on the main listener, unless metrics are off (404) or
	// a dedicated listener is configured (CONSOLE_METRICS_ADDR).
	if s.app.Config.MetricsAddr == "" {
		if h := metricsHandler(s.app.Obs); h != nil {
			s.handle("GET /metrics", h)
		}
	}
}

// handle registers h on the mux and records the pattern for the scope-table
// coverage test.
func (s *Server) handle(pattern string, h http.Handler) {
	s.patterns = append(s.patterns, pattern)
	s.mux.Handle(pattern, h)
}

// handleFunc is handle for a handler function.
func (s *Server) handleFunc(pattern string, h http.HandlerFunc) { s.handle(pattern, h) }

// handleHealthz is the minimal liveness probe: 200 "ok", nothing else.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

// WithLogger sets the logger used for request and panic logs (default
// slog.Default at construction) and returns s. Call before Handler.
func (s *Server) WithLogger(l *slog.Logger) *Server {
	if l != nil {
		s.log = l
	}
	return s
}

// Handler returns the mux wrapped with metrics (outermost), request-ID,
// logging, panic-recovery, security-headers and auth middleware. Request-ID
// sits inside metrics but outside logging/recovery so every log line and auth
// failure carries an ID; auth sits inside metrics so failures are counted. The
// auth middleware is a pass-through unless WithAuthenticator was given one.
func (s *Server) Handler() http.Handler {
	return newHTTPMetrics(s.app.Obs).middleware(
		requestID(logging(s.log, recoverPanic(s.log, securityHeaders(s.authMiddleware(s.mux))))))
}

// securityHeaders sets conservative response headers on every response:
// nosniff, deny framing (clickjacking), a restrictive referrer policy, and a
// CSP that allows the embedded static assets plus the (currently CDN-loaded)
// htmx script. Tighten the CSP once htmx is vendored.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' https://unpkg.com; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

// ListenAndServe serves the dashboard and API on addr until the process exits.
//
// Console serves plain HTTP by design: it binds loopback by default and expects
// TLS to be terminated by a reverse proxy or ingress when exposed to a network
// (see SECURITY.md). It never terminates TLS itself, so the semgrep use-tls rule
// is suppressed here intentionally.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.Handler()) // nosemgrep: go.lang.security.audit.net.use-tls.use-tls
}

// --- middleware ---

// recoverPanic converts a panicking handler into a 500 rather than crashing the
// server, logging the recovered value.
func recoverPanic(l *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			if v := recover(); v != nil {
				l.Error("panic recovered",
					"request_id", RequestID(r.Context()),
					"method", r.Method,
					"route", routeOf(r),
					"status", http.StatusInternalServerError,
					"duration_ms", durationMS(time.Since(start)),
					"panic", fmt.Sprint(v))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// durationMS converts d to float milliseconds so JSON logs carry a
// self-describing number rather than integer nanoseconds.
func durationMS(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// logging logs one structured record per request: request ID, method, route
// pattern (never the raw path), status, and duration_ms. Request bodies, headers
// and query strings are deliberately not logged.
func logging(l *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sr, r)
		l.Info("request",
			"request_id", RequestID(r.Context()),
			"method", r.Method,
			"route", routeOf(r),
			"status", sr.status,
			"duration_ms", durationMS(time.Since(start)))
	})
}

// routeOf returns the matched mux pattern, or "unmatched" (the same bounded
// label the metrics use) when no route matched.
func routeOf(r *http.Request) string {
	if r.Pattern == "" {
		return unmatchedRoute
	}
	return r.Pattern
}

// --- shared JSON helpers ---

// writeJSON encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// writeError maps a core error to an HTTP status and emits a JSON error body.
// core.ErrNotFound → 404, core.ErrConflict → 409, anything else → 500.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errBody{Error: err.Error()})
	case errors.Is(err, core.ErrConflict):
		writeJSON(w, http.StatusConflict, errBody{Error: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errBody{Error: err.Error()})
	}
}

// badRequest emits a 400 with msg.
func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, errBody{Error: msg})
}

// errBody is the JSON shape returned for API errors.
type errBody struct {
	Error string `json:"error"`
}

// maxBodyBytes caps the size of a decoded request body to bound memory use on
// untrusted input.
const maxBodyBytes = 1 << 20 // 1 MiB

// decodeJSON decodes the request body into dst, reporting a malformed body. The
// body is size-limited; an over-limit body fails to decode and is rejected.
func decodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(dst)
}

// --- shared template helper ---

// render executes the named page template (which pulls in the shared "content"
// block defined in that page's file) against data. Each page file is parsed
// fresh on top of a clone of the base layout so the duplicate "content"
// definitions across pages never collide.
func (s *Server) render(w http.ResponseWriter, page, file string, data any) {
	t := template.Must(template.Must(s.tmpl.Clone()).ParseFS(web.Templates, "templates/"+file))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, page, data); err != nil {
		s.log.Error("render failed", "page", page, "err", err)
	}
}

// renderPartial executes a standalone named partial (used for htmx swaps) from
// the given page file against data.
func (s *Server) renderPartial(w http.ResponseWriter, partial, file string, data any) {
	t := template.Must(template.ParseFS(web.Templates, "templates/"+file))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, partial, data); err != nil {
		s.log.Error("render partial failed", "partial", partial, "err", err)
	}
}
