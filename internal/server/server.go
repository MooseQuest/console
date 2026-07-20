// Package server is Console's HTTP layer. It exposes a JSON API over the flag
// and status engines and a small server-rendered dashboard (htmx) on top of the
// same App. It owns no business logic: every handler is a thin adapter that
// decodes a request, calls an engine, and encodes the result.
package server

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/moosequest/console/internal/app"
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
}

// New builds a Server over a. It parses the embedded dashboard templates and
// registers all routes; the returned Server is ready to serve.
func New(a *app.App) *Server {
	s := &Server{
		app:  a,
		mux:  http.NewServeMux(),
		tmpl: parseTemplates(),
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
	// JSON API.
	s.mux.HandleFunc("GET /api/health", s.handleHealth)

	s.mux.HandleFunc("GET /api/flags", s.handleListFlags)
	s.mux.HandleFunc("POST /api/flags", s.handleCreateFlag)
	s.mux.HandleFunc("GET /api/flags/{key}", s.handleGetFlag)
	s.mux.HandleFunc("PUT /api/flags/{key}", s.handleUpdateFlag)
	s.mux.HandleFunc("DELETE /api/flags/{key}", s.handleDeleteFlag)
	s.mux.HandleFunc("POST /api/flags/{key}/evaluate", s.handleEvaluateFlag)

	s.mux.HandleFunc("GET /api/components", s.handleListComponents)
	s.mux.HandleFunc("POST /api/components", s.handleCreateComponent)
	s.mux.HandleFunc("GET /api/components/{key}", s.handleGetComponent)
	s.mux.HandleFunc("PUT /api/components/{key}", s.handleUpdateComponent)
	s.mux.HandleFunc("DELETE /api/components/{key}", s.handleDeleteComponent)
	s.mux.HandleFunc("POST /api/components/{key}/check", s.handleCheckComponent)

	// Dashboard UI.
	s.mux.HandleFunc("GET /{$}", s.handleOverview)
	s.mux.HandleFunc("GET /flags", s.handleFlagsPage)
	s.mux.HandleFunc("POST /flags/{key}/toggle", s.handleToggleFlag)
	s.mux.HandleFunc("GET /status", s.handleStatusPage)
	s.mux.HandleFunc("POST /status/{key}/check", s.handleCheckComponentUI)

	// Static assets (CSS, vendored JS).
	static := http.FileServerFS(web.Static)
	s.mux.Handle("GET /static/", static)
}

// Handler returns the mux wrapped with logging + panic-recovery middleware.
func (s *Server) Handler() http.Handler {
	return logging(recoverPanic(securityHeaders(s.mux)))
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
func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic: %v %s: %v", r.Method, r.URL.Path, v)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
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

// logging logs one line per request: method, path, status, and duration.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sr, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sr.status, time.Since(start))
	})
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
		log.Printf("render %s: %v", page, err)
	}
}

// renderPartial executes a standalone named partial (used for htmx swaps) from
// the given page file against data.
func (s *Server) renderPartial(w http.ResponseWriter, partial, file string, data any) {
	t := template.Must(template.ParseFS(web.Templates, "templates/"+file))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, partial, data); err != nil {
		log.Printf("render partial %s: %v", partial, err)
	}
}
