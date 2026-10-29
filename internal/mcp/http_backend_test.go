package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/server"
)

// TestHTTPBackend exercises the -addr path end to end: it stands up the real
// Console JSON API over httptest and drives it through NewHTTPBackend.
func TestHTTPBackend(t *testing.T) {
	ctx := context.Background()
	a, err := app.New(ctx, config.Config{DB: ""})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer a.Close()

	ts := httptest.NewServer(server.New(a).Handler())
	defer ts.Close()

	b, err := NewHTTPBackend(ts.URL)
	if err != nil {
		t.Fatalf("NewHTTPBackend: %v", err)
	}

	if err := b.CreateFlag(ctx, core.Flag{Key: "beta-x", Scope: core.ScopeAll, Rollout: 100, Enabled: true}); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}

	flags, err := b.ListFlags(ctx)
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if len(flags) != 1 || flags[0].Key != "beta-x" {
		t.Fatalf("expected [beta-x], got %+v", flags)
	}

	ev, err := b.EvaluateFlag(ctx, "beta-x", core.Subject{Key: "user-1"})
	if err != nil {
		t.Fatalf("EvaluateFlag: %v", err)
	}
	if ev.FlagKey != "beta-x" || !ev.Enabled {
		t.Errorf("evaluation = %+v, want enabled beta-x", ev)
	}

	h, err := b.HealthSnapshot(ctx)
	if err != nil {
		t.Fatalf("HealthSnapshot: %v", err)
	}
	if h.State != core.StateUnknown {
		t.Errorf("empty snapshot = %v, want unknown", h.State)
	}

	if err := b.DeleteFlag(ctx, "beta-x"); err != nil {
		t.Fatalf("DeleteFlag: %v", err)
	}
	flags, _ = b.ListFlags(ctx)
	if len(flags) != 0 {
		t.Errorf("flag not deleted, still have %+v", flags)
	}
}

// addr normalization: bare host:port gets an http scheme.
func TestNewHTTPBackend_BareAddr(t *testing.T) {
	if _, err := NewHTTPBackend("127.0.0.1:8080"); err != nil {
		t.Errorf("bare host:port should be accepted: %v", err)
	}
	if _, err := NewHTTPBackend(""); err == nil {
		t.Error("empty addr should error")
	}
}

func TestHTTPBackendToken(t *testing.T) {
	ctx := context.Background()
	var got string
	status := 200
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(status)
		if status == 200 {
			w.Write([]byte("[]"))
		}
	}))
	defer ts.Close()

	b, err := NewHTTPBackend(ts.URL, WithToken("s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ListFlags(ctx); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer s3cret" {
		t.Errorf("Authorization = %q", got)
	}

	status = 403
	_, err = b.ListFlags(ctx)
	if err == nil || !strings.Contains(err.Error(), `"read"`) || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("read 403 error = %v", err)
	}
	err = b.CreateFlag(ctx, core.Flag{Key: "x"})
	if err == nil || !strings.Contains(err.Error(), `"write"`) || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("write 403 error = %v", err)
	}
	status = 401
	nb, _ := NewHTTPBackend(ts.URL)
	if _, err = nb.ListFlags(ctx); err == nil || !strings.Contains(err.Error(), "CONSOLE_TOKEN") {
		t.Errorf("no-token 401 error = %v", err)
	}
}

func TestHTTPBackendInsecureTokenRefused(t *testing.T) {
	_, err := NewHTTPBackend("example.com:8080", WithToken("s3cret"))
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("want refusal without token echo, got %v", err)
	}
	for _, ok := range []string{"127.0.0.1:1", "localhost:1", "[::1]:1", "https://example.com"} {
		if _, err := NewHTTPBackend(ok, WithToken("t")); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	if _, err := NewHTTPBackend("example.com:8080", WithToken("t"), WithInsecureToken(true)); err != nil {
		t.Errorf("insecure override: %v", err)
	}
	if _, err := NewHTTPBackend("example.com:8080"); err != nil {
		t.Errorf("no token: %v", err)
	}
}

func TestResolveToken(t *testing.T) {
	f := filepath.Join(t.TempDir(), "tok")
	os.WriteFile(f, []byte("filetok\n"), 0o600)
	if tok, err := ResolveToken("envtok", f); err != nil || tok != "filetok" {
		t.Errorf("file wins: %q %v", tok, err)
	}
	if tok, _ := ResolveToken(" envtok\n", ""); tok != "envtok" {
		t.Errorf("env: %q", tok)
	}
	if _, err := ResolveToken("", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing file should error")
	}
}
