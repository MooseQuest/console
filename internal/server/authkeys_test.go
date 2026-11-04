package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

type keysEnv struct {
	h    http.Handler
	srv  *Server
	ks   *auth.MemKeyStore
	logs *bytes.Buffer
}

// newKeysEnv serves the API with a Chain authenticator over the static test
// tokens plus an in-memory KeyStore.
func newKeysEnv(t *testing.T, cfg config.Config) *keysEnv {
	t.Helper()
	cfg.Metrics = true
	e := &keysEnv{ks: auth.NewMemKeyStore(), logs: &bytes.Buffer{}}
	a := newMetricsApp(t, obs.NewMemory(), cfg)
	a.Keys = e.ks
	static := auth.NewStatic(map[auth.Scope][]string{
		auth.Eval: {tokEval}, auth.Read: {tokRead}, auth.Write: {tokWrite}, auth.Admin: {tokAdmin},
	})
	e.srv = New(a).WithLogger(NewLogger(e.logs, "text")).WithAuthenticator(auth.NewChain(static, e.ks))
	e.h = e.srv.Handler()
	return e
}

// mintKey stores a key directly and returns its token and id.
func (e *keysEnv) mintKey(t *testing.T, name string, sc auth.Scope) (tok, id string) {
	t.Helper()
	tok, _ = auth.GenerateToken()
	id, _ = auth.NewID()
	k := auth.Key{ID: id, Name: name, Digest: sha256.Sum256([]byte(tok)), Scope: sc, CreatedAt: time.Now().UTC()}
	if err := e.ks.CreateKey(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	return tok, id
}

func (e *keysEnv) req(method, path, body, token string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

// session logs in with token and returns the session cookie and CSRF token.
func (e *keysEnv) session(t *testing.T, token string) (*http.Cookie, string) {
	t.Helper()
	login := httptest.NewRecorder()
	lreq := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"token": {token}}.Encode()))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.h.ServeHTTP(login, lreq)
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("no session cookie (status %d)", login.Code)
	}
	page := httptest.NewRecorder()
	preq := httptest.NewRequest("GET", "/flags", nil)
	preq.Header.Set("Accept", "text/html")
	preq.AddCookie(cookie)
	e.h.ServeHTTP(page, preq)
	m := csrfAttr.FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatal("csrf token not rendered")
	}
	return cookie, m[1]
}

func (e *keysEnv) cookieReq(c *http.Cookie, method, path, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(c)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func decode[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return v
}

var keyRoutes = []struct{ method, path string }{
	{"POST", "/api/auth/keys"}, {"GET", "/api/auth/keys"},
	{"DELETE", "/api/auth/keys/x"}, {"GET", "/api/auth/audit"},
}

func TestKeyAPIScopes(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	for _, rt := range keyRoutes {
		if rr := e.req(rt.method, rt.path, "{}", ""); rr.Code != 401 {
			t.Errorf("%s %s without token = %d", rt.method, rt.path, rr.Code)
		}
		for _, tok := range []string{tokEval, tokRead, tokWrite} {
			if rr := e.req(rt.method, rt.path, "{}", tok); rr.Code != 403 {
				t.Errorf("%s %s with %s = %d", rt.method, rt.path, tok, rr.Code)
			}
		}
	}
}

func TestKeyAPI501WithoutKeyStore(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true})
	h := New(a).WithLogger(NewLogger(&bytes.Buffer{}, "text")).WithAuthenticator(testAuthn()).Handler()
	for _, rt := range keyRoutes {
		if rr := call(h, rt.method, rt.path, tokAdmin); rr.Code != 501 {
			t.Errorf("%s %s = %d, want 501", rt.method, rt.path, rr.Code)
		}
		if rr := call(h, rt.method, rt.path, tokRead); rr.Code != 403 {
			t.Errorf("%s %s read = %d, want 403 (scope checked before 501)", rt.method, rt.path, rr.Code)
		}
	}
}

func TestCreateKey(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	exp := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	rr := e.req("POST", "/api/auth/keys", `{"name":"ci-bot","scope":"read","expires_at":"`+exp+`"}`, tokAdmin,
		RequestIDHeader, "req-create-1")
	if rr.Code != 201 {
		t.Fatalf("create = %d %s", rr.Code, rr.Body)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	v := decode[map[string]any](t, rr)
	tok, _ := v["key"].(string)
	if !strings.HasPrefix(tok, auth.TokenPrefix) {
		t.Fatalf("key = %q", tok)
	}
	if v["name"] != "ci-bot" || v["scope"] != "read" || v["status"] != "active" || v["expires_at"] != exp {
		t.Errorf("body = %v", v)
	}
	if _, ok := v["digest"]; ok {
		t.Error("response has digest")
	}
	// The new token works at its scope and only there.
	if c := e.req("GET", "/api/flags", "", tok).Code; c != 200 {
		t.Errorf("new key read = %d", c)
	}
	if c := e.req("POST", "/api/flags", "{}", tok).Code; c != 403 {
		t.Errorf("new key write = %d", c)
	}
	// Audit row carries the request ID and acting principal.
	evs, _ := e.ks.ListAudit(context.Background(), auth.AuditFilter{})
	if len(evs) != 1 || evs[0].Action != auth.ActionKeyCreate || evs[0].Target != v["id"] ||
		evs[0].RequestID != "req-create-1" || !strings.HasPrefix(evs[0].Principal, "admin-") {
		t.Errorf("audit = %+v", evs)
	}
	if strings.Contains(e.logs.String(), tok) {
		t.Error("token leaked into logs")
	}
	// List never shows the token or digest, and is not cacheable.
	lr := e.req("GET", "/api/auth/keys", "", tokAdmin)
	if strings.Contains(lr.Body.String(), tok) || strings.Contains(strings.ToLower(lr.Body.String()), "digest") {
		t.Errorf("list leaks secret: %s", lr.Body)
	}
	if lr.Header().Get("Cache-Control") != "no-store" {
		t.Error("list not no-store")
	}
	list := decode[[]map[string]any](t, lr)
	if len(list) != 1 || list[0]["name"] != "ci-bot" {
		t.Errorf("list = %v", list)
	}
	if _, ok := list[0]["key"]; ok {
		t.Error("list contains key field")
	}
}

func TestCreateKeyValidation(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	cases := []struct {
		name, body string
		want       int
	}{
		{"missing name", `{"scope":"read"}`, 400},
		{"blank name", `{"name":"  ","scope":"read"}`, 400},
		{"long name", `{"name":"` + strings.Repeat("a", 200) + `","scope":"read"}`, 400},
		{"missing scope", `{"name":"a"}`, 400},
		{"bad scope", `{"name":"a","scope":"root"}`, 400},
		{"past expiry", `{"name":"a","scope":"read","expires_at":"` + past + `"}`, 400},
		{"bad expiry", `{"name":"a","scope":"read","expires_at":"tomorrow"}`, 400},
		{"unknown field", `{"name":"a","scope":"read","extra":1}`, 400},
		{"not json", `nope`, 400},
		{"trailing data", `{"name":"a","scope":"read"}{}`, 400},
		{"too large", `{"name":"a","scope":"read","pad":"` + strings.Repeat("x", 5000) + `"}`, 413},
	}
	for _, c := range cases {
		if rr := e.req("POST", "/api/auth/keys", c.body, tokAdmin); rr.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rr.Code, rr.Body, c.want)
		}
	}
	if ks, _ := e.ks.ListKeys(context.Background()); len(ks) != 0 {
		t.Errorf("invalid requests created %d keys", len(ks))
	}
	okBody := `{"name":"a","scope":"read"}`
	if rr := e.req("POST", "/api/auth/keys", okBody, tokAdmin); rr.Code != 201 {
		t.Fatalf("ok create = %d", rr.Code)
	}
	if rr := e.req("POST", "/api/auth/keys", okBody, tokAdmin); rr.Code != 409 {
		t.Errorf("duplicate name = %d, want 409", rr.Code)
	}
}

func TestCreateKeyScopeCeiling(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	// Only admin reaches the route, so stub a lower-scope principal directly
	// to prove the handler itself enforces the ceiling.
	do := func(sc auth.Scope, want string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/auth/keys", strings.NewReader(`{"name":"n-`+want+sc.String()+`","scope":"`+want+`"}`))
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, auth.Principal{Name: "stub", Scope: sc}))
		rr := httptest.NewRecorder()
		e.srv.handleCreateKey(rr, req)
		return rr
	}
	if rr := do(auth.Write, "admin"); rr.Code != 403 {
		t.Errorf("write minting admin = %d, want 403", rr.Code)
	}
	if rr := do(auth.Write, "write"); rr.Code != 201 {
		t.Errorf("write minting write = %d, want 201", rr.Code)
	}
	if rr := do(auth.Write, "eval"); rr.Code != 201 {
		t.Errorf("write minting eval = %d, want 201", rr.Code)
	}
	if rr := do(auth.Read, "write"); rr.Code != 403 {
		t.Errorf("read minting write = %d, want 403", rr.Code)
	}
	if ks, _ := e.ks.ListKeys(context.Background()); len(ks) != 2 {
		t.Errorf("keys = %d, want 2", len(ks))
	}
	if rr := e.req("POST", "/api/auth/keys", `{"name":"root2","scope":"admin"}`, tokAdmin); rr.Code != 201 {
		t.Errorf("admin minting admin = %d", rr.Code)
	}
}

func TestRevokeKey(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	tok, id := e.mintKey(t, "victim", auth.Read)
	if c := e.req("GET", "/api/flags", "", tok).Code; c != 200 {
		t.Fatalf("pre-revoke = %d", c)
	}
	rr := e.req("DELETE", "/api/auth/keys/"+id, "", tokAdmin, RequestIDHeader, "req-rev-1")
	if rr.Code != 204 || rr.Body.Len() != 0 {
		t.Fatalf("revoke = %d %q", rr.Code, rr.Body)
	}
	if c := e.req("GET", "/api/flags", "", tok).Code; c != 401 {
		t.Errorf("post-revoke = %d, want 401", c)
	}
	if c := e.req("DELETE", "/api/auth/keys/"+id, "", tokAdmin).Code; c != 409 {
		t.Errorf("second revoke = %d, want 409", c)
	}
	if c := e.req("DELETE", "/api/auth/keys/nope", "", tokAdmin).Code; c != 404 {
		t.Errorf("unknown = %d, want 404", c)
	}
	evs, _ := e.ks.ListAudit(context.Background(), auth.AuditFilter{})
	if len(evs) != 1 || evs[0].Action != auth.ActionKeyRevoke || evs[0].Target != id || evs[0].RequestID != "req-rev-1" {
		t.Errorf("audit = %+v", evs)
	}
}

func TestRevokeSelfGuard(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	tok, id := e.mintKey(t, "root-key", auth.Admin)
	if c := e.req("DELETE", "/api/auth/keys/"+id, "", tok).Code; c != 409 {
		t.Fatalf("self revoke = %d, want 409", c)
	}
	for _, v := range []string{"?force=1", "?force=TRUE", "?force=", "?force=false"} {
		if c := e.req("DELETE", "/api/auth/keys/"+id+v, "", tok).Code; c != 409 {
			t.Errorf("%s = %d, want 409 (exact value only)", v, c)
		}
	}
	// Another admin key can revoke it without force.
	tok2, id2 := e.mintKey(t, "other-admin", auth.Admin)
	_, id3 := e.mintKey(t, "third", auth.Read)
	if c := e.req("DELETE", "/api/auth/keys/"+id3, "", tok2).Code; c != 204 {
		t.Errorf("other revoke = %d", c)
	}
	_ = id2
	if c := e.req("DELETE", "/api/auth/keys/"+id+"?force=true", "", tok).Code; c != 204 {
		t.Errorf("forced self revoke = %d, want 204", c)
	}
	if c := e.req("GET", "/api/auth/keys", "", tok).Code; c != 401 {
		t.Errorf("revoked self key still works: %d", c)
	}
}

// A dashboard session backed by a managed key hits the self-revoke guard too.
func TestRevokeSelfGuardViaSession(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	tok, id := e.mintKey(t, "dash-admin", auth.Admin)
	cookie, csrf := e.session(t, tok)
	if c := e.cookieReq(cookie, "DELETE", "/api/auth/keys/"+id, "", csrfHeader, csrf).Code; c != 409 {
		t.Errorf("session self-revoke = %d, want 409", c)
	}
	if c := e.cookieReq(cookie, "DELETE", "/api/auth/keys/"+id+"?force=true", "", csrfHeader, csrf).Code; c != 204 {
		t.Errorf("forced session self-revoke = %d, want 204", c)
	}
}

func TestAuditList(t *testing.T) {
	e := newKeysEnv(t, config.Config{AuditRetentionDays: 90})
	ctx := context.Background()
	now := time.Now().UTC()
	for i, age := range []time.Duration{120 * 24 * time.Hour, 10 * 24 * time.Hour, 2 * time.Hour} {
		_ = e.ks.AppendAudit(ctx, auth.Event{ID: string(rune('a' + i)), At: now.Add(-age),
			Principal: "p", Action: auth.ActionKeyCreate, Target: "t", RequestID: "r" + string(rune('a'+i))})
	}
	list := func(q string) []map[string]any {
		rr := e.req("GET", "/api/auth/audit"+q, "", tokAdmin)
		if rr.Code != 200 {
			t.Fatalf("%s = %d %s", q, rr.Code, rr.Body)
		}
		return decode[[]map[string]any](t, rr)
	}
	got := list("")
	if len(got) != 2 || got[0]["id"] != "c" || got[1]["id"] != "b" {
		t.Fatalf("default (retention floor, newest first) = %v", got)
	}
	if got[0]["request_id"] != "rc" {
		t.Errorf("request_id = %v", got[0]["request_id"])
	}
	if got := list("?since=" + url.QueryEscape(now.Add(-200*24*time.Hour).Format(time.RFC3339))); len(got) != 2 {
		t.Errorf("old since returned %d rows", len(got))
	}
	if got := list("?since=" + url.QueryEscape(now.Add(-24*time.Hour).Format(time.RFC3339))); len(got) != 1 {
		t.Errorf("recent since returned %d rows", len(got))
	}
	if got := list("?limit=1"); len(got) != 1 || got[0]["id"] != "c" {
		t.Errorf("limit=1 = %v", got)
	}
	for _, q := range []string{"?limit=0", "?limit=1001", "?limit=x", "?limit=-1", "?since=yesterday"} {
		if c := e.req("GET", "/api/auth/audit"+q, "", tokAdmin).Code; c != 400 {
			t.Errorf("%s = %d, want 400", q, c)
		}
	}
	if c := e.req("GET", "/api/auth/audit?limit=1000", "", tokAdmin).Code; c != 200 {
		t.Errorf("limit=1000 = %d", c)
	}
	// Retention 0 (keep forever) shows everything.
	e0 := newKeysEnv(t, config.Config{AuditRetentionDays: 0})
	_ = e0.ks.AppendAudit(ctx, auth.Event{ID: "old", At: now.Add(-400 * 24 * time.Hour), Principal: "p", Action: auth.ActionKeyCreate})
	if got := decode[[]map[string]any](t, e0.req("GET", "/api/auth/audit", "", tokAdmin)); len(got) != 1 {
		t.Errorf("retention 0 = %v", got)
	}
	// Empty results are [] not null.
	if b := strings.TrimSpace(e0.req("GET", "/api/auth/keys", "", tokAdmin).Body.String()); b != "[]" {
		t.Errorf("empty keys = %s", b)
	}
}

func TestKeyAPIAuthDisabled(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true})
	ks := auth.NewMemKeyStore()
	a.Keys = ks
	h := New(a).WithLogger(NewLogger(&bytes.Buffer{}, "text")).Handler()
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rr
	}
	rr := do("POST", "/api/auth/keys", `{"name":"a","scope":"admin"}`)
	if rr.Code != 201 {
		t.Fatalf("create = %d %s", rr.Code, rr.Body)
	}
	id := decode[map[string]any](t, rr)["id"].(string)
	if c := do("DELETE", "/api/auth/keys/"+id, "").Code; c != 204 {
		t.Errorf("revoke = %d", c)
	}
	evs, _ := ks.ListAudit(context.Background(), auth.AuditFilter{})
	if len(evs) != 2 {
		t.Fatalf("audit = %+v", evs)
	}
	for _, ev := range evs {
		if ev.Principal != "anonymous" {
			t.Errorf("principal = %q, want anonymous", ev.Principal)
		}
	}
}

func TestKeyAPICSRF(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	cookie, csrf := e.session(t, tokAdmin)
	body := `{"name":"via-cookie","scope":"read"}`
	if c := e.cookieReq(cookie, "POST", "/api/auth/keys", body).Code; c != 403 {
		t.Errorf("cookie without CSRF = %d, want 403", c)
	}
	if c := e.cookieReq(cookie, "POST", "/api/auth/keys", body, csrfHeader, "wrong").Code; c != 403 {
		t.Errorf("cookie with bad CSRF = %d, want 403", c)
	}
	if ks, _ := e.ks.ListKeys(context.Background()); len(ks) != 0 {
		t.Fatal("denied request created a key")
	}
	rr := e.cookieReq(cookie, "POST", "/api/auth/keys", body, csrfHeader, csrf)
	if rr.Code != 201 {
		t.Fatalf("cookie with CSRF = %d %s", rr.Code, rr.Body)
	}
	id := decode[map[string]any](t, rr)["id"].(string)
	if c := e.cookieReq(cookie, "DELETE", "/api/auth/keys/"+id, "").Code; c != 403 {
		t.Errorf("cookie DELETE without CSRF = %d, want 403", c)
	}
	if c := e.cookieReq(cookie, "DELETE", "/api/auth/keys/"+id, "", csrfHeader, csrf).Code; c != 204 {
		t.Errorf("cookie DELETE with CSRF = %d, want 204", c)
	}
	if c := e.cookieReq(cookie, "GET", "/api/auth/keys", "").Code; c != 200 {
		t.Errorf("cookie GET = %d", c)
	}
	if c := e.req("POST", "/api/auth/keys", `{"name":"bearer","scope":"read"}`, tokAdmin).Code; c != 201 {
		t.Errorf("bearer POST = %d", c)
	}
}

func TestKeyAPIFailClosed(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	if c := e.req("GET", "/api/auth/keys/abc", "", tokAdmin).Code; c != 405 {
		t.Errorf("GET /api/auth/keys/{id} = %d, want 405", c)
	}
	if c := e.req("PUT", "/api/auth/keys", "{}", tokAdmin).Code; c != 405 {
		t.Errorf("PUT /api/auth/keys = %d, want 405", c)
	}
	if c := e.req("GET", "/api/auth/nope", "", tokAdmin).Code; c != 404 {
		t.Errorf("unknown = %d, want 404", c)
	}
}
