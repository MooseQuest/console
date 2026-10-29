package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

const (
	tokEval  = "csk_eval-token"
	tokRead  = "csk_read-token"
	tokWrite = "csk_write-token"
	tokAdmin = "csk_admin-token"
)

func testAuthn() auth.Authenticator {
	return auth.NewStatic(map[auth.Scope][]string{
		auth.Eval: {tokEval}, auth.Read: {tokRead},
		auth.Write: {tokWrite}, auth.Admin: {tokAdmin},
	})
}

func authedServer(t *testing.T, rec obs.Recorder, logBuf *bytes.Buffer) http.Handler {
	t.Helper()
	a := newMetricsApp(t, rec, config.Config{Metrics: true})
	return New(a).WithLogger(NewLogger(logBuf, "text")).WithAuthenticator(testAuthn()).Handler()
}

func call(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"key":"k1","name":"n"}`))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// TestRouteScopesCoverMux fails if a registered route has no scope entry, and
// if the table holds an entry for a route that does not exist (other than the
// ones that are conditionally or not yet registered).
func TestRouteScopesCoverMux(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true})
	s := New(a)
	registered := map[string]bool{}
	for _, p := range s.patterns {
		registered[p] = true
		if _, ok := routeScopes[p]; !ok {
			t.Errorf("route %q has no entry in routeScopes", p)
		}
	}
	for _, want := range []string{"GET /metrics", "GET /healthz", "GET /static/"} {
		if !registered[want] {
			t.Errorf("expected %q to be registered", want)
		}
	}
	pending := map[string]bool{"GET /login": true, "POST /login": true}
	for p := range routeScopes {
		if !registered[p] && !pending[p] {
			t.Errorf("routeScopes has stale entry %q", p)
		}
	}
}

func TestAuthDisabledIsPassThrough(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true})
	h := New(a).Handler()
	for _, path := range []string{"/api/health", "/api/flags", "/metrics", "/healthz", "/flags"} {
		if rr := call(h, "GET", path, ""); rr.Code != 200 {
			t.Errorf("GET %s = %d, want 200", path, rr.Code)
		}
	}
}

func TestHealthzUnauthenticated(t *testing.T) {
	h := authedServer(t, obs.NewMemory(), &bytes.Buffer{})
	rr := call(h, "GET", "/healthz", "")
	if rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != "ok" {
		t.Fatalf("healthz = %d %q", rr.Code, rr.Body.String())
	}
	if rr := call(h, "GET", "/static/does-not-exist.css", ""); rr.Code == 401 || rr.Code == 403 {
		t.Fatalf("static must be exempt, got %d", rr.Code)
	}
}

func TestAuthScopes(t *testing.T) {
	h := authedServer(t, obs.NewMemory(), &bytes.Buffer{})
	cases := []struct {
		name, method, path, token string
		want                      int
	}{
		{"no token", "GET", "/api/health", "", 401},
		{"bad token", "GET", "/api/health", "nope", 401},
		{"read ok", "GET", "/api/health", tokRead, 200},
		{"eval cannot read", "GET", "/api/health", tokEval, 403},
		{"metrics needs read", "GET", "/metrics", "", 401},
		{"metrics read", "GET", "/metrics", tokRead, 200},
		{"metrics eval", "GET", "/metrics", tokEval, 403},
		{"read cannot write", "POST", "/api/flags", tokRead, 403},
		{"write ok", "POST", "/api/flags", tokWrite, 201},
		{"eval route eval token", "POST", "/api/flags/k1/evaluate", tokEval, 200},
		{"eval route none", "POST", "/api/flags/k1/evaluate", "", 401},
		{"toggle read", "POST", "/flags/k1/toggle", tokRead, 403},
		{"check read", "POST", "/status/x/check", tokRead, 403},
		{"admin reads", "GET", "/api/flags", tokAdmin, 200},
		{"dashboard unauth", "GET", "/", "", 401},
		{"dashboard read", "GET", "/", tokRead, 200},
	}
	for _, c := range cases {
		rr := call(h, c.method, c.path, c.token)
		if c.name == "eval route eval token" {
			// Handler may legitimately 404/400 for a flag we did not create;
			// only auth outcomes matter here.
			if rr.Code == 401 || rr.Code == 403 {
				t.Errorf("%s: got %d", c.name, rr.Code)
			}
			continue
		}
		if rr.Code != c.want {
			t.Errorf("%s: %s %s = %d, want %d (%s)", c.name, c.method, c.path, rr.Code, c.want, rr.Body.String())
		}
		if c.want == 401 && rr.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("%s: missing WWW-Authenticate", c.name)
		}
	}
}

func TestAuthErrorBodyDoesNotEchoCredential(t *testing.T) {
	h := authedServer(t, obs.NewMemory(), &bytes.Buffer{})
	for _, tok := range []string{"secret-bad-token", tokRead} {
		rr := call(h, "POST", "/api/flags", tok)
		if strings.Contains(rr.Body.String(), tok) || strings.Contains(rr.Header().Get("WWW-Authenticate"), tok) {
			t.Errorf("response echoes credential %q", tok)
		}
	}
}

func TestAuthFailureMetric(t *testing.T) {
	mem := obs.NewMemory()
	h := authedServer(t, mem, &bytes.Buffer{})
	call(h, "GET", "/api/flags", "")
	call(h, "GET", "/api/flags", "bad")
	call(h, "POST", "/api/flags", tokRead)
	rr := call(h, "GET", "/metrics", tokRead)
	body := rr.Body.String()
	if !strings.Contains(body, `console_auth_failures_total{reason="unauthenticated"} 2`) {
		t.Errorf("missing unauthenticated count:\n%s", body)
	}
	if !strings.Contains(body, `console_auth_failures_total{reason="forbidden"} 1`) {
		t.Errorf("missing forbidden count:\n%s", body)
	}
	if strings.Contains(body, "bad") && strings.Contains(body, `reason="bad`) {
		t.Error("token-derived label")
	}
}

func TestPrincipalInContext(t *testing.T) {
	const pat = "GET /test/ctx"
	routeScopes[pat] = need(auth.Read)
	t.Cleanup(func() { delete(routeScopes, pat) })

	a := newMetricsApp(t, obs.NewMemory(), config.Config{})
	s := New(a).WithAuthenticator(testAuthn())
	var got auth.Principal
	var ok bool
	s.handleFunc(pat, func(w http.ResponseWriter, r *http.Request) {
		got, ok = PrincipalFrom(r.Context())
	})
	h := s.Handler()
	if rr := call(h, "GET", "/test/ctx", tokRead); rr.Code != 200 {
		t.Fatalf("got %d", rr.Code)
	}
	if !ok || got.Scope != auth.Read || !strings.HasPrefix(got.Name, "read-1-") {
		t.Fatalf("principal = %+v ok=%v", got, ok)
	}
	if _, ok := PrincipalFrom(httptest.NewRequest("GET", "/", nil).Context()); ok {
		t.Error("principal present without middleware")
	}
}

func TestUnknownRouteFailsClosed(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{})
	s := New(a).WithAuthenticator(testAuthn())
	s.handleFunc("GET /test/unlisted", func(http.ResponseWriter, *http.Request) {})
	if rr := call(s.Handler(), "GET", "/test/unlisted", tokAdmin); rr.Code != 403 {
		t.Fatalf("unlisted route = %d, want 403", rr.Code)
	}
}

// TestDeniedRequestsKeepRoute asserts 401 and 403 responses are logged and
// counted under their matched route pattern, not "unmatched".
func TestDeniedRequestsKeepRoute(t *testing.T) {
	var buf bytes.Buffer
	mem := obs.NewMemory()
	h := authedServer(t, mem, &buf)
	if rr := call(h, "GET", "/api/flags", ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
	if rr := call(h, "POST", "/api/flags", tokRead); rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}

	logs := buf.String()
	for _, want := range []string{
		`route="GET /api/flags" status=401`,
		`route="POST /api/flags" status=403`,
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("request log missing %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "route=unmatched") {
		t.Errorf("request log has unmatched route:\n%s", logs)
	}

	body := call(h, "GET", "/metrics", tokRead).Body.String()
	for _, want := range []string{
		`console_http_requests_total{code="401",method="GET",route="GET /api/flags"} 1`,
		`console_http_requests_total{code="403",method="POST",route="POST /api/flags"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `route="unmatched"`) {
		t.Errorf("metrics have unmatched route:\n%s", body)
	}
}

func TestAuditLog(t *testing.T) {
	var buf bytes.Buffer
	h := authedServer(t, obs.NewMemory(), &buf)
	call(h, "POST", "/api/flags", tokWrite)
	call(h, "POST", "/api/flags", tokRead)
	call(h, "GET", "/api/flags", tokRead)
	out := buf.String()

	var audits []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "msg=audit") {
			audits = append(audits, l)
		}
	}
	if len(audits) != 2 {
		t.Fatalf("want 2 audit lines (mutations only), got %d:\n%s", len(audits), out)
	}
	first := audits[0]
	for _, want := range []string{"principal=write-1-", "scope=write", "method=POST", `route="POST /api/flags"`, "status=201", "request_id="} {
		if !strings.Contains(first, want) {
			t.Errorf("audit line missing %q: %s", want, first)
		}
	}
	if !strings.Contains(audits[1], "scope=read") || !strings.Contains(audits[1], "status=403") {
		t.Errorf("denied audit line wrong: %s", audits[1])
	}
	for _, secret := range []string{tokWrite, tokRead, tokEval, tokAdmin, "k1"} {
		if strings.Contains(out, secret) {
			t.Errorf("log contains secret/body value %q", secret)
		}
	}
}
