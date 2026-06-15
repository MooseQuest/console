package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/flags"
	"github.com/moosequest/console/internal/status"
	"github.com/moosequest/console/internal/store/sqlite"
)

// newTestServer builds an App by hand over an in-memory SQLite store (no
// network, no LLM) and returns an httptest server fronting it. The returned
// cleanup closes both.
func newTestServer(t *testing.T) (*httptest.Server, *app.App) {
	t.Helper()

	st, err := sqlite.Open(context.Background(), "") // empty DSN => shared in-memory DB
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := &app.App{
		Store:  st,
		Flags:  flags.New(st),
		Status: status.New(st, st, &status.HTTPProvider{}),
		// LLM left nil: AI-Assisted mode is unavailable in tests.
	}

	srv := httptest.NewServer(New(a).Handler())
	t.Cleanup(func() {
		srv.Close()
		_ = a.Close()
	})
	return srv, a
}

// do issues a request and returns the response; body may be nil.
func do(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

func TestFlagLifecycleAPI(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create.
	create := core.Flag{
		Key:         "checkout-v2",
		Description: "New checkout flow",
		Enabled:     true,
		Scope:       core.ScopeAll,
		Rollout:     100,
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/flags", create)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create flag: got %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	// List.
	resp = do(t, http.MethodGet, srv.URL+"/api/flags", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list flags: got %d, want 200", resp.StatusCode)
	}
	var list []core.Flag
	decodeBody(t, resp, &list)
	if len(list) != 1 || list[0].Key != "checkout-v2" {
		t.Fatalf("list flags: got %+v, want one checkout-v2 flag", list)
	}

	// Get.
	resp = do(t, http.MethodGet, srv.URL+"/api/flags/checkout-v2", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get flag: got %d, want 200", resp.StatusCode)
	}
	var got core.Flag
	decodeBody(t, resp, &got)
	if got.Key != "checkout-v2" || !got.Enabled {
		t.Fatalf("get flag: got %+v, want enabled checkout-v2", got)
	}

	// Evaluate: enabled, 100% ScopeAll => Enabled true.
	resp = do(t, http.MethodPost, srv.URL+"/api/flags/checkout-v2/evaluate", core.Subject{Key: "user-1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("evaluate flag: got %d, want 200", resp.StatusCode)
	}
	var eval core.Evaluation
	decodeBody(t, resp, &eval)
	if !eval.Enabled {
		t.Fatalf("evaluate flag: got Enabled=false, want true (reason %q)", eval.Reason)
	}
}

func TestGetMissingFlag404(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := do(t, http.MethodGet, srv.URL+"/api/flags/nope", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing flag: got %d, want 404", resp.StatusCode)
	}
}

func TestHealthEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := do(t, http.MethodGet, srv.URL+"/api/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: got %d, want 200", resp.StatusCode)
	}
	var h core.Health
	decodeBody(t, resp, &h)
	// No components yet => aggregate state is Unknown.
	if h.State != core.StateUnknown {
		t.Fatalf("health: got state %v, want unknown", h.State)
	}
}

func TestComponentCreateAndCheck(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a component with the built-in HTTP provider pointed at our own
	// test server, so the check resolves without external network.
	comp := core.Component{
		Key:      "self",
		Name:     "Self",
		Provider: "http",
		Config:   map[string]string{"url": srv.URL + "/api/health"},
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/components", comp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create component: got %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	// Check now.
	resp = do(t, http.MethodPost, srv.URL+"/api/components/self/check", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check component: got %d, want 200", resp.StatusCode)
	}
	var check core.Check
	decodeBody(t, resp, &check)
	if check.Component != "self" {
		t.Fatalf("check component: got component %q, want self", check.Component)
	}
}

func TestOverviewPageRenders(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := do(t, http.MethodGet, srv.URL+"/", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview page: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("overview page: content-type %q, want text/html", ct)
	}
	body := readAll(t, resp)
	for _, want := range []string{"Console", "Overview", "Components"} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview page missing %q", want)
		}
	}
}

func TestFlagsPageRenders(t *testing.T) {
	srv, _ := newTestServer(t)

	// Seed a flag so the table has a row with the htmx toggle.
	resp := do(t, http.MethodPost, srv.URL+"/api/flags", core.Flag{
		Key:     "show-banner",
		Scope:   core.ScopeAll,
		Enabled: true,
		Rollout: 50,
	})
	resp.Body.Close()

	resp = do(t, http.MethodGet, srv.URL+"/flags", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("flags page: got %d, want 200", resp.StatusCode)
	}
	body := readAll(t, resp)
	for _, want := range []string{"show-banner", "Feature flags", "/flags/show-banner/toggle"} {
		if !strings.Contains(body, want) {
			t.Fatalf("flags page missing %q", want)
		}
	}
}

func TestToggleFlagPartial(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := do(t, http.MethodPost, srv.URL+"/api/flags", core.Flag{
		Key:     "t",
		Scope:   core.ScopeAll,
		Enabled: false,
	})
	resp.Body.Close()

	resp = do(t, http.MethodPost, srv.URL+"/flags/t/toggle", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("toggle: got %d, want 200", resp.StatusCode)
	}
	body := readAll(t, resp)
	// After toggling a disabled flag it should now render "On".
	if !strings.Contains(body, "On") || !strings.Contains(body, `id="flag-t"`) {
		t.Fatalf("toggle partial unexpected: %q", body)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
