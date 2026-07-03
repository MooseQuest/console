package mcp

import (
	"context"
	"net/http/httptest"
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
