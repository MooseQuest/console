package plugin

import (
	"context"
	"sync"
	"testing"
	"time"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/status"
)

// recordingProvider captures the component it was asked to check and returns a
// fixed result (server side of the test).
type recordingProvider struct {
	mu  sync.Mutex
	got core.Component
	out core.Check
}

func (p *recordingProvider) Name() string { return "recorder" }

func (p *recordingProvider) Check(_ context.Context, comp core.Component) core.Check {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = comp
	return p.out
}

func TestStatusProviderPlugin_RoundTrip(t *testing.T) {
	backing := &recordingProvider{
		out: core.Check{
			Component: "api",
			State:     core.StateDegraded,
			Message:   "errors 3/100 (3.00%)",
			Latency:   42 * time.Millisecond,
			CheckedAt: time.Unix(1_700_000_000, 0).UTC(),
		},
	}
	client, server := goplugin.TestPluginGRPCConn(t, false, map[string]goplugin.Plugin{
		StatusProviderPluginName: &StatusProviderPlugin{Impl: backing},
	})
	t.Cleanup(func() { _ = client.Close(); server.Stop() })

	raw, err := client.Dispense(StatusProviderPluginName)
	if err != nil {
		t.Fatalf("dispense: %v", err)
	}
	p, ok := raw.(status.Provider)
	if !ok {
		t.Fatalf("dispensed %T, want status.Provider", raw)
	}

	if p.Name() != "recorder" {
		t.Errorf("Name() over gRPC = %q, want recorder", p.Name())
	}

	comp := core.Component{
		Key:      "api",
		Name:     "API",
		Provider: "cloudflare-workers",
		Config:   map[string]string{"worker": "edge"},
	}
	got := p.Check(context.Background(), comp)

	if got.Component != "api" || got.State != core.StateDegraded ||
		got.Message != "errors 3/100 (3.00%)" || got.Latency != 42*time.Millisecond {
		t.Fatalf("check did not round-trip: %+v", got)
	}
	if !got.CheckedAt.Equal(backing.out.CheckedAt) {
		t.Errorf("CheckedAt = %v, want %v", got.CheckedAt, backing.out.CheckedAt)
	}

	backing.mu.Lock()
	defer backing.mu.Unlock()
	if backing.got.Key != "api" || backing.got.Provider != "cloudflare-workers" ||
		backing.got.Config["worker"] != "edge" {
		t.Fatalf("component did not arrive at provider: %+v", backing.got)
	}
}
