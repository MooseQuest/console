package server

import (
	"crypto/sha256"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/config"
)

// sessionCookie is the name of the dashboard session cookie. Its value is the
// random 256-bit session ID; nothing else is stored client-side.
const sessionCookie = "console_session"

// maxLoginBody caps the login form body.
const maxLoginBody = 4 << 10

// loginData drives the login page. It never carries the submitted token.
type loginData struct {
	Title string
	// Error is set after a rejected token (one generic message for any reason).
	Error bool
	// Throttled is set while the client is locked out; RetryAfter is the wait
	// in whole seconds.
	Throttled  bool
	RetryAfter int
	// SignedOut is set after a logout.
	SignedOut bool
}

// handleLoginPage renders the login form.
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.authn == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if _, ok := s.sessionFromRequest(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderLogin(w, http.StatusOK, loginData{SignedOut: r.URL.Query().Get("signed_out") == "1"})
}

// handleLogin exchanges a token for a session cookie. The token is read from
// the POST body only (never the URL), compared via the Authenticator in
// constant time, and never logged. Every rejection shows the same message.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.authn == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	key := clientKey(r)
	if wait := s.throttle.Check(key); wait > 0 {
		secs := int((wait + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		s.renderLogin(w, http.StatusTooManyRequests, loginData{Throttled: true, RetryAfter: secs})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
	token := ""
	if err := r.ParseForm(); err == nil {
		token = strings.TrimSpace(r.PostForm.Get("token"))
	}

	p, err := s.authenticateToken(r, token)
	if err != nil {
		if !errors.Is(err, auth.ErrUnauthenticated) {
			s.log.Error("authenticator error", "request_id", RequestID(r.Context()))
		}
		s.throttle.Fail(key)
		s.log.Warn("login failed", "request_id", RequestID(r.Context()))
		s.renderLogin(w, http.StatusUnauthorized, loginData{Error: true})
		return
	}
	s.throttle.Success(key)
	id, err := s.sessions.Create(p, sha256.Sum256([]byte(token)))
	if err != nil {
		s.log.Error("session create failed", "request_id", RequestID(r.Context()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.log.Info("login", "principal", p.Name, "scope", p.Scope.String(), "request_id", RequestID(r.Context()))
	http.SetCookie(w, s.cookie(r, id, int(auth.SessionMaxLifetime/time.Second)))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLogout destroys the caller's session (if any) and clears the cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.Delete(c.Value)
	}
	http.SetCookie(w, s.cookie(r, "", -1))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/login?signed_out=1", http.StatusSeeOther)
}

// authenticateToken runs token through the configured Authenticator by
// presenting it as a bearer credential on a clone of r.
func (s *Server) authenticateToken(r *http.Request, token string) (auth.Principal, error) {
	if token == "" {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+token)
	return s.authn.Authenticate(r2)
}

// sessionFromRequest resolves the request's session cookie to a live session
// whose login token is still configured. A session whose token was removed is
// destroyed. The returned principal is the freshly resolved one, so a changed
// scope takes effect immediately. It fails closed if the Authenticator cannot
// re-check digests.
func (s *Server) sessionFromRequest(r *http.Request) (auth.Principal, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.Principal{}, false
	}
	sess, ok := s.sessions.Get(c.Value)
	if !ok {
		return auth.Principal{}, false
	}
	da, ok := s.authn.(auth.DigestAuthenticator)
	if !ok {
		s.sessions.Delete(c.Value)
		return auth.Principal{}, false
	}
	p, err := da.AuthenticateDigest(sess.Digest)
	if err != nil {
		s.sessions.Delete(c.Value)
		return auth.Principal{}, false
	}
	return p, true
}

// cookie builds the session cookie: HttpOnly, SameSite=Lax, Path=/, and Secure
// per CONSOLE_COOKIE_SECURE.
func (s *Server) cookie(r *http.Request, value string, maxAge int) *http.Cookie {
	secure := s.app.Config.CookieSecure == config.CookieSecureAlways ||
		r.TLS != nil ||
		strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	}
}

// clientKey is the throttle key: the connection's remote IP. X-Forwarded-For is
// deliberately ignored because any client can forge it.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// wantsHTML reports whether r is a browser page navigation (as opposed to an
// API client): a GET/HEAD outside /api/ that accepts text/html.
func wantsHTML(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func (s *Server) renderLogin(w http.ResponseWriter, status int, d loginData) {
	d.Title = "Sign in"
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, "login", "login.html", d)
}
