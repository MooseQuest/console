package plugin

import (
	"context"
	"sync"
	"testing"
	"time"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/notify"
)

// recordingNotifier captures delivered events (server side of the test).
type recordingNotifier struct {
	mu  sync.Mutex
	got []core.Event
}

func (n *recordingNotifier) Name() string { return "recorder" }

func (n *recordingNotifier) Notify(_ context.Context, ev core.Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.got = append(n.got, ev)
	return nil
}

func TestNotifierPlugin_RoundTrip(t *testing.T) {
	backing := &recordingNotifier{}
	client, server := goplugin.TestPluginGRPCConn(t, false, map[string]goplugin.Plugin{
		NotifierPluginName: &NotifierPlugin{Impl: backing},
	})
	t.Cleanup(func() { _ = client.Close(); server.Stop() })

	raw, err := client.Dispense(NotifierPluginName)
	if err != nil {
		t.Fatalf("dispense: %v", err)
	}
	n, ok := raw.(notify.Notifier)
	if !ok {
		t.Fatalf("dispensed %T, want notify.Notifier", raw)
	}

	if n.Name() != "recorder" {
		t.Errorf("Name() over gRPC = %q, want recorder", n.Name())
	}

	ev := core.Event{
		Type: core.EventComponentDown, Title: "api is down", Message: "8% errors",
		Component: "api", From: core.StateOperational, To: core.StateDown,
		At: time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := n.Notify(context.Background(), ev); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	backing.mu.Lock()
	defer backing.mu.Unlock()
	if len(backing.got) != 1 {
		t.Fatalf("backing notifier received %d events, want 1", len(backing.got))
	}
	got := backing.got[0]
	if got.Type != core.EventComponentDown || got.Title != "api is down" ||
		got.Component != "api" || got.From != core.StateOperational || got.To != core.StateDown {
		t.Fatalf("event did not round-trip: %+v", got)
	}
	if !got.At.Equal(ev.At) {
		t.Errorf("At = %v, want %v", got.At, ev.At)
	}
}
