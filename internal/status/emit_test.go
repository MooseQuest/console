package status

import (
	"context"
	"sync"
	"testing"

	"github.com/moosequest/console/internal/core"
)

// recorder collects emitted events.
type recorder struct {
	mu  sync.Mutex
	evs []core.Event
}

func (r *recorder) emit(ev core.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, ev)
}

func (r *recorder) types() []core.EventType {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.EventType, len(r.evs))
	for i, e := range r.evs {
		out[i] = e.Type
	}
	return out
}

func TestEngineEmitsOnTransitions(t *testing.T) {
	st := newFakeStore()
	p := &fakeProvider{name: "fake", state: core.StateOperational}
	e := New(st, st, p)
	rec := &recorder{}
	e.SetEmitter(rec.emit)

	comp := core.Component{Key: "api", Name: "API", Provider: "fake"}
	ctx := context.Background()

	e.Run(ctx, comp) // unknown -> operational: quiet
	p.state = core.StateDown
	e.Run(ctx, comp) // operational -> down
	e.Run(ctx, comp) // down -> down: no repeat
	p.state = core.StateOperational
	e.Run(ctx, comp) // down -> operational: recovered
	p.state = core.StateDegraded
	e.Run(ctx, comp) // operational -> degraded

	got := rec.types()
	want := []core.EventType{
		core.EventComponentDown,
		core.EventComponentRecovered,
		core.EventComponentDegraded,
	}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] = %s, want %s (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestEngineFirstCheckDownAlerts(t *testing.T) {
	st := newFakeStore()
	p := &fakeProvider{name: "fake", state: core.StateDown}
	e := New(st, st, p)
	rec := &recorder{}
	e.SetEmitter(rec.emit)

	e.Run(context.Background(), core.Component{Key: "api", Provider: "fake"})
	if types := rec.types(); len(types) != 1 || types[0] != core.EventComponentDown {
		t.Fatalf("first-check-down should emit one component_down, got %v", types)
	}
}

func TestEngineNoEmitterIsSafe(t *testing.T) {
	st := newFakeStore()
	e := New(st, st, &fakeProvider{name: "fake", state: core.StateDown})
	// No SetEmitter: Run must not panic and still records.
	e.Run(context.Background(), core.Component{Key: "api", Provider: "fake"})
	if st.recordCount() != 1 {
		t.Fatalf("expected check recorded, got %d", st.recordCount())
	}
}
