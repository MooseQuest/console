package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/config"
)

const formCT = "Content-Type"
const formMIME = "application/x-www-form-urlencoded"

// uiAdmin logs in as the static admin token and returns the cookie and CSRF.
func uiAdmin(t *testing.T, e *keysEnv) (*http.Cookie, string) {
	t.Helper()
	return e.session(t, tokAdmin)
}

// uiPost posts a form (adding the csrf_token field when csrf != "").
func (e *keysEnv) uiPost(c *http.Cookie, csrf, path string, v url.Values, hdr ...string) *httptest.ResponseRecorder {
	if v == nil {
		v = url.Values{}
	}
	if csrf != "" {
		v.Set("csrf_token", csrf)
	}
	return e.cookieReq(c, "POST", path, v.Encode(), append([]string{formCT, formMIME}, hdr...)...)
}

func (e *keysEnv) keyByName(t *testing.T, name string) (auth.Key, bool) {
	t.Helper()
	keys, _ := e.ks.ListKeys(context.Background())
	for _, k := range keys {
		if k.Name == name {
			return k, true
		}
	}
	return auth.Key{}, false
}

var secretRe = regexp.MustCompile(`id="secret"[^>]*value="(csk_[^"]+)"`)

func TestKeysScopeMatrix(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	_, id := e.mintKey(t, "k", auth.Read)
	routes := []struct{ method, path string }{
		{"GET", "/keys"}, {"POST", "/keys"}, {"POST", "/keys/" + id + "/revoke"}, {"GET", "/keys/audit"},
	}
	for _, rt := range routes {
		// Anonymous browser navigation: 303 to /login. Other anonymous: 401.
		if rt.method == "GET" {
			rr := e.req(rt.method, rt.path, "", "", "Accept", "text/html")
			if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
				t.Errorf("anonymous %s %s = %d %q", rt.method, rt.path, rr.Code, rr.Header().Get("Location"))
			}
		}
		for _, tok := range []string{tokEval, tokRead, tokWrite} {
			if rr := e.req(rt.method, rt.path, "", tok); rr.Code != http.StatusForbidden {
				t.Errorf("%s %s with %s = %d, want 403", rt.method, rt.path, tok, rr.Code)
			}
		}
	}
	// Read/write sessions get 403 on the page; admin gets 200.
	for _, tok := range []string{tokRead, tokWrite} {
		c, _ := e.session(t, tok)
		if rr := e.cookieReq(c, "GET", "/keys", ""); rr.Code != http.StatusForbidden {
			t.Errorf("session %s GET /keys = %d, want 403", tok, rr.Code)
		}
		if rr := e.cookieReq(c, "GET", "/flags", "", "Accept", "text/html"); strings.Contains(rr.Body.String(), `href="/keys"`) {
			t.Errorf("nav link shown to non-admin session %s", tok)
		}
	}
	c, _ := uiAdmin(t, e)
	for _, p := range []string{"/keys", "/keys/audit"} {
		if rr := e.cookieReq(c, "GET", p, ""); rr.Code != http.StatusOK {
			t.Errorf("admin GET %s = %d", p, rr.Code)
		}
	}
	if rr := e.cookieReq(c, "GET", "/flags", "", "Accept", "text/html"); !strings.Contains(rr.Body.String(), `href="/keys"`) {
		t.Error("nav link missing for admin")
	}
}

func TestKeysCSRFRejectedOnEveryPost(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	_, id := e.mintKey(t, "victim", auth.Read)
	c, tok := uiAdmin(t, e)
	posts := []struct {
		path string
		form url.Values
	}{
		{"/keys", url.Values{"name": {"evil"}, "scope": {"read"}}},
		{"/keys/" + id + "/revoke", url.Values{"confirm": {"1"}}},
	}
	for _, p := range posts {
		cases := map[string]*httptest.ResponseRecorder{
			"missing":    e.uiPost(c, "", p.path, cloneVals(p.form)),
			"wrong":      e.uiPost(c, "nope", p.path, cloneVals(p.form)),
			"cross-site": e.uiPost(c, tok, p.path, cloneVals(p.form), "Sec-Fetch-Site", "cross-site"),
			"bad-origin": e.uiPost(c, tok, p.path, cloneVals(p.form), "Origin", "https://evil.example"),
		}
		for name, rr := range cases {
			if rr.Code != http.StatusForbidden {
				t.Errorf("%s %s = %d, want 403", name, p.path, rr.Code)
			}
		}
	}
	if _, ok := e.keyByName(t, "evil"); ok {
		t.Error("key created despite CSRF failure")
	}
	if k, _ := e.keyByName(t, "victim"); k.Revoked() {
		t.Error("key revoked despite CSRF failure")
	}
}

func cloneVals(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func TestKeysCreateShowsSecretOnce(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	c, tok := uiAdmin(t, e)
	rr := e.uiPost(c, tok, "/keys", url.Values{"name": {"ci-bot"}, "scope": {"read"}, "expires": {time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")}})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rr.Code, rr.Body.String())
	}
	m := secretRe.FindStringSubmatch(rr.Body.String())
	if m == nil {
		t.Fatalf("no secret in body: %s", rr.Body.String())
	}
	secret := m[1]
	if n := strings.Count(rr.Body.String(), secret); n != 1 {
		t.Errorf("secret appears %d times, want 1", n)
	}
	h := rr.Header()
	if h.Get("Cache-Control") != "no-store" || h.Get("HX-Push-Url") != "false" {
		t.Errorf("headers: %v", h)
	}
	if !strings.Contains(rr.Body.String(), `hx-history="false"`) || !strings.Contains(rr.Body.String(), "/static/keys.js") {
		t.Error("missing hx-history=false or keys.js")
	}
	// Not in URLs, cookies or redirects.
	if len(rr.Result().Cookies()) != 0 || h.Get("Location") != "" || strings.Contains(h.Get("Set-Cookie"), secret) {
		t.Error("secret leaked via cookie or redirect")
	}
	// The token authenticates at the chosen scope.
	if got := e.req("GET", "/api/flags", "", secret).Code; got != 200 {
		t.Errorf("new key read access = %d", got)
	}
	if got := e.req("POST", "/api/flags", `{"key":"x","name":"n"}`, secret).Code; got != http.StatusForbidden {
		t.Errorf("read key write access = %d, want 403", got)
	}
	// A following GET shows neither token nor digest, and the key is listed.
	k, ok := e.keyByName(t, "ci-bot")
	if !ok {
		t.Fatal("key not stored")
	}
	g := e.cookieReq(c, "GET", "/keys", "")
	body := g.Body.String()
	if strings.Contains(body, secret) || strings.Contains(body, "csk_") || !strings.Contains(body, "ci-bot") {
		t.Error("list leaks token or omits key")
	}
	if g.Header().Get("Cache-Control") != "no-store" || strings.Contains(g.Header().Get("HX-Push-Url"), "false") {
		t.Errorf("GET headers: %v", g.Header())
	}
	// key.create event with the session principal and request ID.
	evs, _ := e.ks.ListAudit(context.Background(), auth.AuditFilter{})
	found := false
	for _, ev := range evs {
		if ev.Action == auth.ActionKeyCreate && ev.Target == k.ID {
			found = true
			if ev.Principal == "" || ev.Principal == "anonymous" || ev.RequestID == "" {
				t.Errorf("event = %+v", ev)
			}
		}
	}
	if !found {
		t.Error("no key.create event")
	}
	if !strings.Contains(body, "key.create") || !strings.Contains(body, "ci-bot") {
		t.Error("audit table should list the create event")
	}
}

func TestKeysCreateErrors(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	c, tok := uiAdmin(t, e)
	e.mintKey(t, "taken", auth.Read)
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	cases := []struct {
		name string
		form url.Values
		code int
		want string
	}{
		{"empty name", url.Values{"name": {" "}, "scope": {"read"}}, 400, "Name is required"},
		{"long name", url.Values{"name": {strings.Repeat("a", 129)}, "scope": {"read"}}, 400, "longer than"},
		{"bad scope", url.Values{"name": {"x"}, "scope": {"root"}}, 400, "Scope must be"},
		{"past expiry", url.Values{"name": {"x"}, "scope": {"read"}, "expires": {yesterday}}, 400, "Expiry must be in the future"},
		{"bad date", url.Values{"name": {"x"}, "scope": {"read"}, "expires": {"soon"}}, 400, "Expiry must be a date"},
		{"duplicate", url.Values{"name": {"taken"}, "scope": {"read"}}, 409, "already exists"},
		{"escaped", url.Values{"name": {"<script>alert(1)</script>"}, "scope": {"root"}, "expires": {tomorrow}}, 400, "&lt;script&gt;alert(1)"},
	}
	for _, tc := range cases {
		rr := e.uiPost(c, tok, "/keys", tc.form)
		if rr.Code != tc.code || !strings.Contains(rr.Body.String(), tc.want) {
			t.Errorf("%s: %d, want %d containing %q", tc.name, rr.Code, tc.code, tc.want)
		}
		if strings.Contains(rr.Body.String(), "csk_") || strings.Contains(rr.Body.String(), "<script>alert") {
			t.Errorf("%s: token or unescaped markup in body", tc.name)
		}
		if !strings.Contains(rr.Body.String(), `role="alert"`) || !strings.Contains(rr.Body.String(), `aria-invalid="true"`) {
			t.Errorf("%s: missing alert/aria-invalid", tc.name)
		}
	}
	// Values are retained.
	rr := e.uiPost(c, tok, "/keys", url.Values{"name": {"keepme"}, "scope": {"write"}, "expires": {yesterday}})
	if !strings.Contains(rr.Body.String(), `value="keepme"`) || !strings.Contains(rr.Body.String(), `<option value="write" selected>`) {
		t.Error("form values not retained")
	}
	// Oversized body, via header token and via form token.
	big := url.Values{"name": {strings.Repeat("a", 5000)}, "scope": {"read"}}
	if rr := e.uiPost(c, tok, "/keys", big); rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized = %d, want 413", rr.Code)
	}
	if rr := e.cookieReq(c, "POST", "/keys", big.Encode(), formCT, formMIME, "X-CSRF-Token", tok); rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized (header token) = %d, want 413", rr.Code)
	}
}

func TestKeysCreateScopeCeiling(t *testing.T) {
	// A write-scope caller cannot reach the page (admin only), so exercise the
	// ceiling through the handler with an injected principal.
	e := newKeysEnv(t, config.Config{})
	req := httptest.NewRequest("POST", "/keys", strings.NewReader(url.Values{"name": {"up"}, "scope": {"admin"}}.Encode()))
	req.Header.Set(formCT, formMIME)
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, auth.Principal{Name: "w", Scope: auth.Write}))
	rr := httptest.NewRecorder()
	e.srv.handleKeysCreate(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "higher scope") {
		t.Errorf("ceiling = %d: %s", rr.Code, rr.Body.String())
	}
	if _, ok := e.keyByName(t, "up"); ok {
		t.Error("key created above ceiling")
	}
	if strings.Contains(rr.Body.String(), `<option value="admin"`) {
		t.Error("options above the caller's scope should be omitted")
	}
}

func TestKeysRevokeTwoStep(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	c, tok := uiAdmin(t, e)
	_, id := e.mintKey(t, "doomed", auth.Read)
	path := "/keys/" + id + "/revoke"

	rr := e.uiPost(c, tok, path, nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Revoke key doomed?") || !strings.Contains(rr.Body.String(), `name="confirm" value="1"`) {
		t.Fatalf("step 1 = %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sign you out") || strings.Contains(rr.Body.String(), "Revoke and sign out") {
		t.Error("self warning shown for another key")
	}
	if k, _ := e.keyByName(t, "doomed"); k.Revoked() {
		t.Fatal("step 1 revoked the key")
	}

	rr = e.uiPost(c, tok, path, url.Values{"confirm": {"1"}})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/keys?revoked=1" {
		t.Fatalf("step 2 = %d %q", rr.Code, rr.Header().Get("Location"))
	}
	if k, _ := e.keyByName(t, "doomed"); !k.Revoked() {
		t.Fatal("step 2 did not revoke")
	}
	g := e.cookieReq(c, "GET", "/keys?revoked=1", "")
	if !strings.Contains(g.Body.String(), "Key revoked.") || !strings.Contains(g.Body.String(), "key.revoke") {
		t.Error("revoked note or audit event missing")
	}
	// Repeat 409, unknown 404, and the session survives.
	if rr := e.uiPost(c, tok, path, url.Values{"confirm": {"1"}}); rr.Code != http.StatusConflict {
		t.Errorf("repeat = %d, want 409", rr.Code)
	}
	if rr := e.uiPost(c, tok, "/keys/nope/revoke", url.Values{"confirm": {"1"}}); rr.Code != http.StatusNotFound {
		t.Errorf("unknown = %d, want 404", rr.Code)
	}
	if rr := e.cookieReq(c, "GET", "/keys", ""); rr.Code != 200 {
		t.Errorf("session lost after revoking another key: %d", rr.Code)
	}
}

func TestKeysSelfRevokeEndsSession(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	adminTok, id := e.mintKey(t, "my-admin", auth.Admin)
	c, tok := e.session(t, adminTok)
	path := "/keys/" + id + "/revoke"

	rr := e.uiPost(c, tok, path, nil)
	b := rr.Body.String()
	if rr.Code != 200 || !strings.Contains(b, "signs you out immediately") || !strings.Contains(b, "Revoke and sign out") || !strings.Contains(b, `role="alert"`) {
		t.Fatalf("step 1 self = %d: %s", rr.Code, b)
	}
	// A forged "self" field changes nothing: the server decides.
	other, oid := e.mintKey(t, "other", auth.Read)
	_ = other
	if b := e.uiPost(c, tok, "/keys/"+oid+"/revoke", url.Values{"self": {"1"}}).Body.String(); strings.Contains(b, "Revoke and sign out") {
		t.Error("client-supplied self flag was trusted")
	}

	rr = e.uiPost(c, tok, path, url.Values{"confirm": {"1"}})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login?signed_out=1&key_revoked=1" {
		t.Fatalf("step 2 self = %d %q", rr.Code, rr.Header().Get("Location"))
	}
	cleared := false
	for _, ck := range rr.Result().Cookies() {
		if ck.Name == sessionCookie && ck.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("session cookie not cleared")
	}
	if got := e.cookieReq(c, "GET", "/keys", "", "Accept", "text/html"); got.Code != http.StatusSeeOther || got.Header().Get("Location") != "/login" {
		t.Errorf("old cookie after self-revoke = %d", got.Code)
	}
	lp := e.req("GET", "/login?signed_out=1&key_revoked=1", "", "")
	if !strings.Contains(lp.Body.String(), "Your key was revoked, so you were signed out.") || strings.Contains(lp.Body.String(), "You have been signed out") {
		t.Errorf("login note: %s", lp.Body.String())
	}
}

func TestKeysEnvTokenSessionNeverSelf(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	_, id := e.mintKey(t, "x", auth.Read)
	c, tok := uiAdmin(t, e)
	b := e.uiPost(c, tok, "/keys/"+id+"/revoke", nil).Body.String()
	if strings.Contains(b, "signs you out") {
		t.Error("env-token session saw the self warning")
	}
}

func TestKeysAuditPresets(t *testing.T) {
	e := newKeysEnv(t, config.Config{AuditRetentionDays: 90})
	c, _ := uiAdmin(t, e)
	now := time.Now().UTC()
	add := func(id string, age time.Duration, target string) {
		_ = e.ks.AppendAudit(context.Background(), auth.Event{ID: id, At: now.Add(-age), Principal: "p", Action: auth.ActionKeyCreate, Target: target, RequestID: "req-" + id})
	}
	add("e1", 30*time.Minute, "t-recent")
	add("e2", 5*time.Hour, "t-5h")
	add("e3", 3*24*time.Hour, "t-3d")
	add("e4", 30*24*time.Hour, "t-30d")
	add("e5", 200*24*time.Hour, "t-200d") // beyond retention: never shown
	for _, tc := range []struct {
		since string
		want  []string
		not   []string
	}{
		{"1h", []string{"t-recent"}, []string{"t-5h", "t-3d", "t-30d"}},
		{"24h", []string{"t-recent", "t-5h"}, []string{"t-3d"}},
		{"", []string{"t-recent", "t-5h"}, []string{"t-3d"}},
		{"7d", []string{"t-recent", "t-5h", "t-3d"}, []string{"t-30d"}},
		{"all", []string{"t-recent", "t-3d", "t-30d"}, []string{"t-200d"}},
	} {
		for _, path := range []string{"/keys?since=" + tc.since, "/keys/audit?since=" + tc.since} {
			rr := e.cookieReq(c, "GET", path, "")
			b := rr.Body.String()
			if rr.Code != 200 {
				t.Fatalf("%s = %d", path, rr.Code)
			}
			for _, w := range tc.want {
				if !strings.Contains(b, w) {
					t.Errorf("%s missing %s", path, w)
				}
			}
			for _, w := range tc.not {
				if strings.Contains(b, w) {
					t.Errorf("%s should not contain %s", path, w)
				}
			}
			if !strings.Contains(b, "Audit events are kept for 90 days.") {
				t.Errorf("%s: retention line missing", path)
			}
		}
	}
	// Current preset is marked.
	b := e.cookieReq(c, "GET", "/keys/audit?since=7d", "").Body.String()
	if !regexp.MustCompile(`hx-get="/keys/audit\?since=7d"[^>]*aria-current="true"`).MatchString(b) {
		t.Errorf("current preset not marked: %s", b)
	}
	// Fragment has no page shell.
	frag := e.cookieReq(c, "GET", "/keys/audit", "", "HX-Request", "true").Body.String()
	if strings.Contains(frag, "<html") || strings.Contains(frag, "<nav") {
		t.Error("fragment contains page shell")
	}
	// Bad since.
	for _, p := range []string{"/keys?since=2w", "/keys/audit?since=2w", "/keys/audit?since=2020-01-01T00:00:00Z"} {
		if rr := e.cookieReq(c, "GET", p, ""); rr.Code != 400 {
			t.Errorf("%s = %d, want 400", p, rr.Code)
		}
	}
	// Unauthenticated htmx GET redirects the client.
	rr := e.req("GET", "/keys/audit?since=1h", "", "", "HX-Request", "true")
	if rr.Header().Get("HX-Redirect") != "/login" {
		t.Errorf("unauthenticated htmx = %d %v", rr.Code, rr.Header())
	}
}

func TestKeysAuditRetentionForeverAndLimit(t *testing.T) {
	e := newKeysEnv(t, config.Config{AuditRetentionDays: 0})
	c, _ := uiAdmin(t, e)
	b := e.cookieReq(c, "GET", "/keys/audit?since=all", "").Body.String()
	if !strings.Contains(b, "kept forever") || !strings.Contains(b, "No key events in this period.") || strings.Contains(b, "Showing the latest") {
		t.Errorf("empty/forever: %s", b)
	}
	for i := 0; i < defaultAuditLimit+5; i++ {
		_ = e.ks.AppendAudit(context.Background(), auth.Event{ID: "id" + time.Now().Add(time.Duration(i)).Format("150405.000000000"), At: time.Now().UTC(), Principal: "p", Action: auth.ActionKeyRevoke, Target: "t"})
	}
	b = e.cookieReq(c, "GET", "/keys/audit?since=all", "").Body.String()
	if !strings.Contains(b, "Showing the latest 100 events.") || strings.Count(b, "key.revoke") != defaultAuditLimit {
		t.Errorf("limit: %d rows", strings.Count(b, "key.revoke"))
	}
}

func TestKeysEmptyList(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	c, _ := uiAdmin(t, e)
	b := e.cookieReq(c, "GET", "/keys", "").Body.String()
	if !strings.Contains(b, "No managed keys yet. Create one above.") || !strings.Contains(b, "No key events in this period.") ||
		!strings.Contains(b, "environment variables are not listed") {
		t.Error("empty states missing")
	}
}

func TestKeysUnavailable501(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	e.srv.app.Keys = nil
	c, tok := uiAdmin(t, e)
	for _, rt := range []struct{ m, p string }{{"GET", "/keys"}, {"POST", "/keys"}, {"POST", "/keys/x/revoke"}, {"GET", "/keys/audit"}} {
		var rr *httptest.ResponseRecorder
		if rt.m == "GET" {
			rr = e.cookieReq(c, rt.m, rt.p, "")
		} else {
			rr = e.uiPost(c, tok, rt.p, nil)
		}
		b := rr.Body.String()
		if rr.Code != http.StatusNotImplemented || strings.Contains(b, "<form method=\"post\" action=\"/keys") || !strings.Contains(b, "SQLite") ||
			!strings.Contains(b, "<html") {
			t.Errorf("%s %s = %d", rt.m, rt.p, rr.Code)
		}
	}
}

type failingKeys struct {
	*auth.MemKeyStore
	failAudit, failList bool
}

func (f failingKeys) AppendAudit(ctx context.Context, e auth.Event) error {
	if f.failAudit {
		return errors.New("disk on fire")
	}
	return f.MemKeyStore.AppendAudit(ctx, e)
}

func (f failingKeys) ListKeys(ctx context.Context) ([]auth.Key, error) {
	if f.failList {
		return nil, errors.New("disk on fire")
	}
	return f.MemKeyStore.ListKeys(ctx)
}

func TestKeysStoreFailures(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	c, tok := uiAdmin(t, e)

	e.srv.app.Keys = failingKeys{MemKeyStore: e.ks, failList: true}
	rr := e.uiPost(c, tok, "/keys", url.Values{"name": {"made"}, "scope": {"read"}})
	// Creation succeeded and the secret is still delivered; the list failed.
	if rr.Code != http.StatusCreated || !secretRe.MatchString(rr.Body.String()) || !strings.Contains(rr.Body.String(), "could not be loaded") {
		t.Errorf("create with list failure = %d", rr.Code)
	}
	_, id := e.mintKey(t, "r", auth.Read)
	rr = e.uiPost(c, tok, "/keys/"+id+"/revoke", nil)
	if rr.Code != 500 || strings.Contains(rr.Body.String(), "disk on fire") || !strings.Contains(rr.Body.String(), "Request ID") {
		t.Errorf("list failure = %d: %s", rr.Code, rr.Body.String())
	}

	e.srv.app.Keys = failingKeys{MemKeyStore: e.ks, failAudit: true}
	rr = e.uiPost(c, tok, "/keys", url.Values{"name": {"made2"}, "scope": {"read"}})
	if rr.Code != http.StatusCreated || !strings.Contains(rr.Body.String(), "audit event could not be recorded") {
		t.Errorf("audit failure on create = %d", rr.Code)
	}
	rr = e.uiPost(c, tok, "/keys/"+id+"/revoke", url.Values{"confirm": {"1"}})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "audit event could not be recorded") {
		t.Errorf("audit failure on revoke = %d", rr.Code)
	}
	if k, _ := e.keyByName(t, "r"); !k.Revoked() {
		t.Error("revoke should stand when audit fails")
	}
}

func TestKeysPagesCSPAndMarkup(t *testing.T) {
	e := newKeysEnv(t, config.Config{})
	c, tok := uiAdmin(t, e)
	_, id := e.mintKey(t, "markup", auth.Read)
	pages := map[string]*httptest.ResponseRecorder{
		"list":    e.cookieReq(c, "GET", "/keys", ""),
		"created": e.uiPost(c, tok, "/keys", url.Values{"name": {"p"}, "scope": {"read"}}),
		"confirm": e.uiPost(c, tok, "/keys/"+id+"/revoke", nil),
	}
	wantCSP := "default-src 'self'; script-src 'self' https://unpkg.com; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'"
	inline := regexp.MustCompile(`(?i)<script(\s[^>]*)?>\s*[^<\s]|\son[a-z]+=|\sstyle=|hx-on`)
	for name, rr := range pages {
		b := rr.Body.String()
		if rr.Header().Get("Content-Security-Policy") != wantCSP {
			t.Errorf("%s: CSP changed", name)
		}
		if inline.MatchString(b) {
			t.Errorf("%s: inline script/handler/style: %s", name, inline.FindString(b))
		}
		// Every input/select has a matching label (hidden inputs excepted).
		for _, m := range regexp.MustCompile(`<(?:input|select)\b[^>]*\bid="([^"]+)"`).FindAllStringSubmatch(b, -1) {
			if !strings.Contains(b, `for="`+m[1]+`"`) {
				t.Errorf("%s: control %s has no label", name, m[1])
			}
		}
		if name != "confirm" && (!strings.Contains(b, `<th scope="col">`) || !strings.Contains(b, `aria-label="Revoke key markup"`)) {
			t.Errorf("%s: missing th scope or revoke aria-label", name)
		}
	}
	if !strings.Contains(pages["created"].Body.String(), `role="status"`) || !strings.Contains(pages["created"].Body.String(), "autofocus") {
		t.Error("created banner missing role=status/autofocus")
	}
	if strings.Contains(pages["list"].Body.String(), "keys.js") {
		t.Error("keys.js loaded on the plain list")
	}
}

func TestKeysAuthDisabled(t *testing.T) {
	a := newMetricsApp(t, nil, config.Config{})
	a.Keys = auth.NewMemKeyStore()
	h := New(a).WithLogger(NewLogger(&strings.Builder{}, "text")).Handler()
	do := func(method, path string, v url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(v.Encode()))
		req.Header.Set(formCT, formMIME)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	if rr := do("GET", "/keys", nil); rr.Code != 200 || strings.Contains(rr.Body.String(), "csrf_token") || !strings.Contains(rr.Body.String(), `href="/keys"`) {
		t.Fatalf("GET = %d", rr.Code)
	}
	if rr := do("POST", "/keys", url.Values{"name": {"open"}, "scope": {"admin"}}); rr.Code != 201 {
		t.Fatalf("create = %d: %s", rr.Code, rr.Body.String())
	}
	evs, _ := a.Keys.ListAudit(context.Background(), auth.AuditFilter{})
	if len(evs) != 1 || evs[0].Principal != "anonymous" {
		t.Errorf("events = %+v", evs)
	}
}
