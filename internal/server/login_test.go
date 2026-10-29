package server

import (
	"bytes"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

// swapAuthn lets a test replace the configured token set mid-flight.
type swapAuthn struct {
	mu sync.Mutex
	s  *auth.Static
}

func (a *swapAuthn) set(s *auth.Static) { a.mu.Lock(); a.s = s; a.mu.Unlock() }
func (a *swapAuthn) cur() *auth.Static  { a.mu.Lock(); defer a.mu.Unlock(); return a.s }
func (a *swapAuthn) Authenticate(r *http.Request) (auth.Principal, error) {
	return a.cur().Authenticate(r)
}
func (a *swapAuthn) AuthenticateDigest(d [sha256.Size]byte) (auth.Principal, error) {
	return a.cur().AuthenticateDigest(d)
}

type loginEnv struct {
	h     http.Handler
	srv   *Server
	authn *swapAuthn
	logs  *bytes.Buffer
	now   time.Time
}

type testClock struct{ t *time.Time }

func (c testClock) now() time.Time { return *c.t }

func newLoginEnv(t *testing.T, cfg config.Config) *loginEnv {
	t.Helper()
	cfg.Metrics = true
	e := &loginEnv{logs: &bytes.Buffer{}, now: time.Unix(1_700_000_000, 0)}
	e.authn = &swapAuthn{s: auth.NewStatic(map[auth.Scope][]string{
		auth.Read: {tokRead}, auth.Write: {tokWrite},
	})}
	a := newMetricsApp(t, obs.NewMemory(), cfg)
	e.srv = New(a).WithLogger(NewLogger(e.logs, "text")).WithAuthenticator(e.authn)
	clk := testClock{&e.now}
	e.srv.sessions = auth.NewSessionStore(clk.now)
	e.srv.throttle = auth.NewThrottle(clk.now)
	e.h = e.srv.Handler()
	return e
}

func (e *loginEnv) do(method, path string, form url.Values, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	req.RemoteAddr = "192.0.2.7:4444"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func (e *loginEnv) login(tok string, hdr map[string]string) *httptest.ResponseRecorder {
	return e.do("POST", "/login", url.Values{"token": {tok}}, hdr)
}

func sessionCookieOf(rr *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

var htmlAccept = map[string]string{"Accept": "text/html,application/xhtml+xml"}

func TestLoginPageRenders(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	rr := e.do("GET", "/login", nil, htmlAccept)
	body := rr.Body.String()
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	for _, want := range []string{`<label for="token">`, `type="password"`, `autocomplete="off"`, `method="post"`, `action="/login"`} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %q", want)
		}
	}
	if strings.Contains(body, "<script") {
		t.Error("login page must not contain script (CSP)")
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Error("login page should be no-store")
	}
}

func TestLoginSuccessAndSession(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	rr := e.login(tokWrite, nil)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
		t.Fatalf("got %d -> %q", rr.Code, rr.Header().Get("Location"))
	}
	c := sessionCookieOf(rr)
	if c == nil {
		t.Fatal("no session cookie")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure {
		t.Errorf("cookie flags wrong: %+v", c)
	}
	if len(c.Value) != 43 {
		t.Errorf("session id length %d", len(c.Value))
	}

	// Cookie authenticates dashboard pages (read scope) and shows Sign out.
	pg := e.do("GET", "/flags", nil, htmlAccept, c)
	if pg.Code != 200 || !strings.Contains(pg.Body.String(), "Sign out") {
		t.Fatalf("dashboard with session: %d", pg.Code)
	}
	// And API calls, with scope enforced from the token's scope.
	if got := e.do("GET", "/api/flags", nil, nil, c).Code; got != 200 {
		t.Errorf("api with session = %d", got)
	}
	// A logged-in visitor to /login is sent home.
	if got := e.do("GET", "/login", nil, htmlAccept, c).Code; got != http.StatusSeeOther {
		t.Errorf("GET /login with session = %d", got)
	}
}

func TestLoginScopeFromToken(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	c := sessionCookieOf(e.login(tokRead, nil))
	if got := e.do("GET", "/api/flags", nil, nil, c).Code; got != 200 {
		t.Errorf("read session GET = %d", got)
	}
	if got := e.do("POST", "/api/flags", url.Values{}, nil, c).Code; got != 403 {
		t.Errorf("read session POST = %d, want 403", got)
	}
}

func TestLoginBadToken(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	const secret = "csk_definitely-wrong-token"
	for _, tok := range []string{secret, ""} {
		rr := e.login(tok, nil)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", rr.Code)
		}
		if sessionCookieOf(rr) != nil {
			t.Fatal("cookie set on failure")
		}
		if !strings.Contains(rr.Body.String(), "That token was not accepted.") {
			t.Error("generic message missing")
		}
		if strings.Contains(rr.Body.String(), secret) {
			t.Error("submitted token echoed")
		}
	}
	if strings.Contains(e.logs.String(), secret) {
		t.Error("submitted token was logged")
	}
}

func TestLoginTokenInURLIgnored(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	rr := e.do("POST", "/login?token="+tokWrite, url.Values{}, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("token in query accepted: %d", rr.Code)
	}
}

func TestUnauthenticatedRedirectVsAPI401(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	rr := e.do("GET", "/flags", nil, htmlAccept)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
		t.Errorf("html: %d -> %q", rr.Code, rr.Header().Get("Location"))
	}
	rr = e.do("GET", "/api/flags", nil, htmlAccept)
	if rr.Code != 401 || rr.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("api with html accept: %d", rr.Code)
	}
	rr = e.do("GET", "/api/flags", nil, map[string]string{"Accept": "application/json"})
	if rr.Code != 401 {
		t.Errorf("api: %d", rr.Code)
	}
	rr = e.do("POST", "/flags/x/toggle", nil, map[string]string{"HX-Request": "true"})
	if rr.Code != 401 || rr.Header().Get("HX-Redirect") != "/login" {
		t.Errorf("htmx: %d %q", rr.Code, rr.Header().Get("HX-Redirect"))
	}
	// Bad cookie behaves as no cookie.
	rr = e.do("GET", "/", nil, htmlAccept, &http.Cookie{Name: sessionCookie, Value: "bogus"})
	if rr.Code != http.StatusSeeOther {
		t.Errorf("bogus cookie: %d", rr.Code)
	}
}

func TestBearerStillWorks(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	if got := e.do("GET", "/api/flags", nil, map[string]string{"Authorization": "Bearer " + tokRead}).Code; got != 200 {
		t.Errorf("bearer = %d", got)
	}
}

func TestLogout(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	c, tok := csrfSession(t, e)
	rr := e.do("POST", "/logout", url.Values{"csrf_token": {tok}}, nil, c)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login?signed_out=1" {
		t.Fatalf("logout: %d -> %q", rr.Code, rr.Header().Get("Location"))
	}
	if cl := sessionCookieOf(rr); cl == nil || cl.MaxAge >= 0 {
		t.Error("cookie not cleared")
	}
	if got := e.do("GET", "/flags", nil, htmlAccept, c).Code; got != http.StatusSeeOther {
		t.Errorf("old cookie still valid after logout: %d", got)
	}
	if !strings.Contains(e.do("GET", "/login?signed_out=1", nil, htmlAccept).Body.String(), "signed out") {
		t.Error("signed-out notice missing")
	}
}

func TestSessionExpiry(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	c := sessionCookieOf(e.login(tokWrite, nil))
	e.now = e.now.Add(auth.SessionIdleTimeout - time.Minute)
	if got := e.do("GET", "/flags", nil, htmlAccept, c).Code; got != 200 {
		t.Fatalf("active session rejected: %d", got)
	}
	e.now = e.now.Add(auth.SessionIdleTimeout + time.Minute)
	if got := e.do("GET", "/flags", nil, htmlAccept, c).Code; got != http.StatusSeeOther {
		t.Fatalf("idle-expired session accepted: %d", got)
	}
	if e.srv.sessions.Len() != 0 {
		t.Error("expired session not purged")
	}
}

func TestRevocationByTokenRemoval(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	cw := sessionCookieOf(e.login(tokWrite, nil))
	cr := sessionCookieOf(e.login(tokRead, nil))
	// Remove the write token; read remains.
	e.authn.set(auth.NewStatic(map[auth.Scope][]string{auth.Read: {tokRead}}))
	if got := e.do("GET", "/flags", nil, htmlAccept, cw).Code; got != http.StatusSeeOther {
		t.Errorf("session of removed token still valid: %d", got)
	}
	if e.srv.sessions.Len() != 1 {
		t.Errorf("revoked session not destroyed (len %d)", e.srv.sessions.Len())
	}
	if got := e.do("GET", "/flags", nil, htmlAccept, cr).Code; got != 200 {
		t.Errorf("unrelated session revoked: %d", got)
	}
}

func TestLoginThrottling(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	for i := 0; i < 3; i++ {
		if got := e.login("bad", nil).Code; got != 401 {
			t.Fatalf("attempt %d = %d", i+1, got)
		}
	}
	// 4th failure still evaluated (401) but triggers the lockout.
	if got := e.login("bad", nil).Code; got != 401 {
		t.Fatalf("4th = %d", got)
	}
	rr := e.login(tokWrite, nil) // even the right token is refused while locked
	if rr.Code != http.StatusTooManyRequests || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("locked = %d, Retry-After %q", rr.Code, rr.Header().Get("Retry-After"))
	}
	if sessionCookieOf(rr) != nil {
		t.Error("session issued while throttled")
	}
	if !strings.Contains(rr.Body.String(), "Too many failed attempts") {
		t.Error("throttled message missing")
	}
	// Another client is unaffected.
	req := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"token": {tokWrite}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.9:1"
	other := httptest.NewRecorder()
	e.h.ServeHTTP(other, req)
	if other.Code != http.StatusSeeOther {
		t.Errorf("other client = %d", other.Code)
	}
	// After the delay a correct login succeeds and clears the record.
	e.now = e.now.Add(time.Minute)
	if got := e.login(tokWrite, nil).Code; got != http.StatusSeeOther {
		t.Errorf("after delay = %d", got)
	}
}

func TestCookieSecure(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	if c := sessionCookieOf(e.login(tokWrite, map[string]string{"X-Forwarded-Proto": "https"})); c == nil || !c.Secure {
		t.Error("Secure expected with X-Forwarded-Proto: https")
	}
	e = newLoginEnv(t, config.Config{CookieSecure: config.CookieSecureAlways})
	if c := sessionCookieOf(e.login(tokWrite, nil)); c == nil || !c.Secure {
		t.Error("Secure expected with CONSOLE_COOKIE_SECURE=always")
	}
}

func TestLoginRoutesOpenWhenAuthDisabled(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true})
	h := New(a).Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/login", nil))
	if rr.Code != http.StatusSeeOther {
		t.Errorf("GET /login with auth off = %d", rr.Code)
	}
}
