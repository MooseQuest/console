package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/config"
)

var csrfAttr = regexp.MustCompile(`hx-headers='\{"X-CSRF-Token": "([A-Za-z0-9_-]+)"\}'`)

// csrfSession logs in with the write token and returns the cookie and the CSRF
// token as rendered into the dashboard.
func csrfSession(t *testing.T, e *loginEnv) (*http.Cookie, string) {
	t.Helper()
	c := sessionCookieOf(e.login(tokWrite, nil))
	if c == nil {
		t.Fatal("no session cookie")
	}
	pg := e.do("GET", "/flags", nil, htmlAccept, c)
	m := csrfAttr.FindStringSubmatch(pg.Body.String())
	if m == nil {
		t.Fatalf("hx-headers CSRF attribute missing from page (status %d)", pg.Code)
	}
	if len(m[1]) != 43 {
		t.Errorf("csrf token length %d", len(m[1]))
	}
	return c, m[1]
}

func (e *loginEnv) doJSON(method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func (e *loginEnv) post(path string, hdr map[string]string, form url.Values, c *http.Cookie) int {
	if c == nil {
		return e.do("POST", path, form, hdr).Code
	}
	return e.do("POST", path, form, hdr, c).Code
}

var writeJSONHdr = map[string]string{"Authorization": "Bearer " + tokWrite, "Content-Type": "application/json"}

func TestCSRFTokenPerSessionAndRendered(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	c1, t1 := csrfSession(t, e)
	_, t2 := csrfSession(t, e)
	if t1 == t2 {
		t.Error("csrf tokens must differ per session")
	}
	pg := e.do("GET", "/status", nil, htmlAccept, c1)
	if m := csrfAttr.FindStringSubmatch(pg.Body.String()); m == nil || m[1] != t1 {
		t.Error("token not stable across pages in one session")
	}
	if !strings.Contains(pg.Body.String(), `name="csrf_token" value="`+t1+`"`) {
		t.Error("logout form missing hidden csrf field")
	}
	if strings.Contains(c1.String(), t1) {
		t.Error("csrf token leaked into cookie")
	}
}

func TestCSRFDashboardPOSTs(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	for _, tc := range []struct{ path, body string }{
		{"/api/flags", `{"key":"k1","name":"n"}`},
		{"/api/components", `{"key":"c1","name":"c","kind":"http","target":"http://127.0.0.1:1"}`},
	} {
		if rr := e.doJSON("POST", tc.path, tc.body, writeJSONHdr); rr.Code >= 300 {
			t.Fatalf("seed %s: %d %s", tc.path, rr.Code, rr.Body.String())
		}
	}
	c, tok := csrfSession(t, e)
	hx := func(extra map[string]string) map[string]string {
		h := map[string]string{"HX-Request": "true"}
		for k, v := range extra {
			h[k] = v
		}
		return h
	}
	for _, path := range []string{"/flags/k1/toggle", "/status/c1/check"} {
		t.Run(path, func(t *testing.T) {
			if got := e.post(path, hx(nil), nil, c); got != http.StatusForbidden {
				t.Errorf("missing token = %d, want 403", got)
			}
			if got := e.post(path, hx(map[string]string{"X-CSRF-Token": "forged"}), nil, c); got != http.StatusForbidden {
				t.Errorf("forged token = %d, want 403", got)
			}
			if got := e.post(path, hx(map[string]string{"X-CSRF-Token": tok[:42]}), nil, c); got != http.StatusForbidden {
				t.Errorf("truncated token = %d, want 403", got)
			}
			if rr := e.do("POST", path, nil, hx(map[string]string{"X-CSRF-Token": tok}), c); rr.Code != 200 {
				t.Errorf("valid header token = %d: %s", rr.Code, rr.Body.String())
			}
			if got := e.post(path, nil, url.Values{"csrf_token": {tok}}, c); got != 200 {
				t.Errorf("valid form token = %d", got)
			}
			bad := e.do("POST", path, nil, hx(map[string]string{"X-CSRF-Token": "forged-xyz"}), c)
			if strings.Contains(bad.Body.String(), "forged-xyz") || strings.Contains(bad.Body.String(), tok) {
				t.Error("403 body echoes a token")
			}
		})
	}
}

func TestCSRFCrossOriginRejected(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	e.doJSON("POST", "/api/flags", `{"key":"k1","name":"n"}`, writeJSONHdr)
	c, tok := csrfSession(t, e)
	for name, hdr := range map[string]map[string]string{
		"origin":         {"Origin": "https://evil.example"},
		"sec-fetch-site": {"Sec-Fetch-Site": "cross-site"},
	} {
		h := map[string]string{"X-CSRF-Token": tok}
		for k, v := range hdr {
			h[k] = v
		}
		if got := e.post("/flags/k1/toggle", h, nil, c); got != http.StatusForbidden {
			t.Errorf("%s with valid token = %d, want 403", name, got)
		}
	}
	h := map[string]string{"X-CSRF-Token": tok, "Sec-Fetch-Site": "same-origin", "Origin": "http://example.com"}
	if got := e.post("/flags/k1/toggle", h, nil, c); got != 200 {
		t.Errorf("same-origin = %d, want 200", got)
	}
	if !strings.Contains(e.logs.String(), "csrf check failed") || strings.Contains(e.logs.String(), tok) {
		t.Error("failure must be logged, without the token")
	}
}

func TestCSRFBearerExempt(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	if rr := e.doJSON("POST", "/api/flags", `{"key":"b1","name":"n"}`, writeJSONHdr); rr.Code != http.StatusCreated {
		t.Errorf("bearer POST without CSRF token = %d: %s", rr.Code, rr.Body.String())
	}
	if rr := e.doJSON("DELETE", "/api/flags/b1", "", writeJSONHdr); rr.Code >= 300 {
		t.Errorf("bearer DELETE = %d", rr.Code)
	}
	// A bearer request that also carries a session cookie is still a bearer
	// request: no CSRF check.
	c, _ := csrfSession(t, e)
	if got := e.do("POST", "/api/flags", nil, writeJSONHdr, c).Code; got == http.StatusForbidden {
		t.Error("bearer request with cookie was CSRF checked")
	}
}

func TestCSRFSafeMethodsUnchecked(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	c, _ := csrfSession(t, e)
	h := map[string]string{"Origin": "https://evil.example", "Accept": "text/html"}
	if got := e.do("GET", "/flags", nil, h, c).Code; got != 200 {
		t.Errorf("GET = %d, want 200", got)
	}
}

func TestCSRFLogout(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	c, tok := csrfSession(t, e)
	if got := e.post("/logout", nil, url.Values{}, c); got != http.StatusForbidden {
		t.Errorf("logout without token = %d, want 403", got)
	}
	if got := e.post("/logout", map[string]string{"Sec-Fetch-Site": "cross-site"}, url.Values{"csrf_token": {tok}}, c); got != http.StatusForbidden {
		t.Errorf("cross-site logout = %d, want 403", got)
	}
	if got := e.do("GET", "/flags", nil, htmlAccept, c).Code; got != 200 {
		t.Errorf("session should survive forged logout, got %d", got)
	}
	if got := e.post("/logout", nil, url.Values{"csrf_token": {tok}}, c); got != http.StatusSeeOther {
		t.Errorf("logout with token = %d, want 303", got)
	}
	if got := e.do("GET", "/flags", nil, htmlAccept, c).Code; got != http.StatusSeeOther {
		t.Errorf("session should be gone after logout, got %d", got)
	}
	// Without a session, logout is harmless and allowed.
	if got := e.post("/logout", nil, url.Values{}, nil); got != http.StatusSeeOther {
		t.Errorf("sessionless logout = %d, want 303", got)
	}
}

func TestCSRFFailuresCounted(t *testing.T) {
	e := newLoginEnv(t, config.Config{})
	c, _ := csrfSession(t, e)
	e.post("/flags/k1/toggle", nil, nil, c)
	rr := e.doJSON("GET", "/metrics", "", map[string]string{"Authorization": "Bearer " + tokRead})
	if !strings.Contains(rr.Body.String(), `console_csrf_failures_total{reason="bad_token"} 1`) {
		t.Errorf("csrf failure not counted:\n%s", rr.Body.String())
	}
}
