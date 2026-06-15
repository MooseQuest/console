package flags

import (
	"context"
	"testing"

	"github.com/moosequest/console/internal/core"
)

func TestEngineEmitsFlagChanges(t *testing.T) {
	e := New(newFakeStore())
	var got []core.Event
	e.SetEmitter(func(ev core.Event) { got = append(got, ev) })

	ctx := context.Background()
	f := core.Flag{Key: "new-ui", Enabled: true, Scope: core.ScopeAll, Rollout: 50}

	if err := e.Create(ctx, f); err != nil {
		t.Fatal(err)
	}
	f.Rollout = 100
	if err := e.Update(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := e.Delete(ctx, f.Key); err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 flag_changed events, got %d (%v)", len(got), got)
	}
	for _, ev := range got {
		if ev.Type != core.EventFlagChanged {
			t.Errorf("type = %s, want flag_changed", ev.Type)
		}
		if ev.Flag != "new-ui" {
			t.Errorf("flag = %q, want new-ui", ev.Flag)
		}
	}
}

func TestEngineNoEmitOnError(t *testing.T) {
	e := New(newFakeStore())
	var count int
	e.SetEmitter(func(core.Event) { count++ })

	ctx := context.Background()
	// Update of a non-existent flag should error and emit nothing.
	if err := e.Update(ctx, core.Flag{Key: "ghost"}); err == nil {
		t.Fatal("expected error updating missing flag")
	}
	if count != 0 {
		t.Fatalf("expected no events on error, got %d", count)
	}
}
