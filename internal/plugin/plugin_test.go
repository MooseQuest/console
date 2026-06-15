package plugin

import (
	"context"
	"errors"
	"testing"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/store"
	"github.com/moosequest/console/internal/store/sqlite"
)

// dialStore wires the server + client adapters over a real in-process gRPC
// connection (no subprocess), backed by an in-memory SQLite store. This
// exercises the full path: client adapter -> gRPC -> server adapter -> store.
func dialStore(t *testing.T) store.Store {
	t.Helper()
	backing, err := sqlite.Open(context.Background(), "")
	if err != nil {
		t.Fatalf("open backing store: %v", err)
	}
	client, server := goplugin.TestPluginGRPCConn(t, false, map[string]goplugin.Plugin{
		StorePluginName: &StorePlugin{Impl: backing},
	})
	t.Cleanup(func() { _ = client.Close(); server.Stop() })

	raw, err := client.Dispense(StorePluginName)
	if err != nil {
		t.Fatalf("dispense: %v", err)
	}
	st, ok := raw.(store.Store)
	if !ok {
		t.Fatalf("dispensed %T, want store.Store", raw)
	}
	return st
}

func TestStorePlugin_FlagRoundTrip(t *testing.T) {
	st := dialStore(t)
	ctx := context.Background()

	in := core.Flag{
		Key: "new-ui", Description: "New UI", Enabled: true,
		Scope: core.ScopeCohort, Rollout: 42, Cohort: "power",
		Variants: []core.Variant{{Key: "a", Value: "1", Weight: 70}, {Key: "b", Value: "2", Weight: 30}},
	}
	if err := st.CreateFlag(ctx, in); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}

	got, err := st.GetFlag(ctx, "new-ui")
	if err != nil {
		t.Fatalf("GetFlag: %v", err)
	}
	if got.Key != in.Key || got.Enabled != in.Enabled || got.Scope != in.Scope ||
		got.Rollout != in.Rollout || got.Cohort != in.Cohort || len(got.Variants) != 2 {
		t.Fatalf("flag did not round-trip: %+v", got)
	}
	if got.Variants[0].Key != "a" || got.Variants[0].Weight != 70 || got.Variants[1].Value != "2" {
		t.Fatalf("variants did not round-trip: %+v", got.Variants)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be set across the wire")
	}
}

func TestStorePlugin_ErrorMapping(t *testing.T) {
	st := dialStore(t)
	ctx := context.Background()

	// NotFound survives the boundary as core.ErrNotFound.
	if _, err := st.GetFlag(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetFlag(missing) = %v, want core.ErrNotFound", err)
	}

	// Conflict survives as core.ErrConflict.
	f := core.Flag{Key: "dup", Scope: core.ScopeAll}
	if err := st.CreateFlag(ctx, f); err != nil {
		t.Fatalf("first CreateFlag: %v", err)
	}
	if err := st.CreateFlag(ctx, f); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("duplicate CreateFlag = %v, want core.ErrConflict", err)
	}
}

func TestStorePlugin_ComponentAndCheckRoundTrip(t *testing.T) {
	st := dialStore(t)
	ctx := context.Background()

	comp := core.Component{Key: "api", Name: "API", Provider: "http", Config: map[string]string{"url": "https://x", "timeout": "2s"}}
	if err := st.CreateComponent(ctx, comp); err != nil {
		t.Fatalf("CreateComponent: %v", err)
	}
	got, err := st.GetComponent(ctx, "api")
	if err != nil {
		t.Fatalf("GetComponent: %v", err)
	}
	if got.Provider != "http" || got.Config["url"] != "https://x" || got.Config["timeout"] != "2s" {
		t.Fatalf("component config did not round-trip: %+v", got)
	}

	ck := core.Check{Component: "api", State: core.StateDegraded, Message: "slow", Latency: 1234}
	if err := st.RecordCheck(ctx, ck); err != nil {
		t.Fatalf("RecordCheck: %v", err)
	}
	latest, err := st.LatestCheck(ctx, "api")
	if err != nil {
		t.Fatalf("LatestCheck: %v", err)
	}
	if latest.State != core.StateDegraded || latest.Message != "slow" || latest.Latency != 1234 {
		t.Fatalf("check did not round-trip: %+v", latest)
	}

	if err := st.Ping(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}
}
