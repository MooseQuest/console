package status

import (
	"context"
	"sync"
	"testing"

	"github.com/moosequest/console/internal/core"
)

// fakeStore is an in-memory ComponentStore + CheckStore for tests. It is safe
// for concurrent use so it can back RunAll's concurrent probes.
type fakeStore struct {
	mu      sync.Mutex
	comps   map[string]core.Component
	checks  map[string]core.Check // latest per component
	records []core.Check          // every recorded check
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		comps:  make(map[string]core.Component),
		checks: make(map[string]core.Check),
	}
}

func (s *fakeStore) CreateComponent(_ context.Context, c core.Component) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.comps[c.Key]; ok {
		return core.ErrConflict
	}
	s.comps[c.Key] = c
	return nil
}

func (s *fakeStore) GetComponent(_ context.Context, key string) (core.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.comps[key]
	if !ok {
		return core.Component{}, core.ErrNotFound
	}
	return c, nil
}

func (s *fakeStore) ListComponents(_ context.Context) ([]core.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.Component, 0, len(s.comps))
	for _, c := range s.comps {
		out = append(out, c)
	}
	return out, nil
}

func (s *fakeStore) UpdateComponent(_ context.Context, c core.Component) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.comps[c.Key]; !ok {
		return core.ErrNotFound
	}
	s.comps[c.Key] = c
	return nil
}

func (s *fakeStore) DeleteComponent(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.comps[key]; !ok {
		return core.ErrNotFound
	}
	delete(s.comps, key)
	return nil
}

func (s *fakeStore) RecordCheck(_ context.Context, c core.Check) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks[c.Component] = c
	s.records = append(s.records, c)
	return nil
}

func (s *fakeStore) LatestCheck(_ context.Context, component string) (core.Check, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.checks[component]
	if !ok {
		return core.Check{}, core.ErrNotFound
	}
	return c, nil
}

func (s *fakeStore) LatestChecks(_ context.Context) ([]core.Check, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.Check, 0, len(s.checks))
	for _, c := range s.checks {
		out = append(out, c)
	}
	return out, nil
}

func (s *fakeStore) recordCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// fakeProvider returns a scripted state for every component.
type fakeProvider struct {
	name  string
	state core.HealthState
}

func (p *fakeProvider) Name() string { return p.name }

func (p *fakeProvider) Check(_ context.Context, comp core.Component) core.Check {
	return core.Check{Component: comp.Key, State: p.state, Message: "scripted"}
}

func TestEngineRunRecordsCheck(t *testing.T) {
	st := newFakeStore()
	e := New(st, st, &fakeProvider{name: "fake", state: core.StateOperational})

	comp := core.Component{Key: "api", Provider: "fake"}
	check := e.Run(context.Background(), comp)

	if check.State != core.StateOperational {
		t.Fatalf("State = %v, want Operational", check.State)
	}
	if st.recordCount() != 1 {
		t.Fatalf("recorded %d checks, want 1", st.recordCount())
	}
	latest, err := st.LatestCheck(context.Background(), "api")
	if err != nil {
		t.Fatalf("LatestCheck: %v", err)
	}
	if latest.State != core.StateOperational {
		t.Fatalf("latest State = %v, want Operational", latest.State)
	}
}

func TestEngineRunUnknownProvider(t *testing.T) {
	st := newFakeStore()
	e := New(st, st)

	check := e.Run(context.Background(), core.Component{Key: "api", Provider: "nope"})
	if check.State != core.StateUnknown {
		t.Fatalf("State = %v, want Unknown", check.State)
	}
	if st.recordCount() != 1 {
		t.Fatalf("recorded %d checks, want 1 (unknown provider still records)", st.recordCount())
	}
}

func TestEngineRunAll(t *testing.T) {
	st := newFakeStore()
	e := New(st, st, &fakeProvider{name: "fake", state: core.StateOperational})

	for _, key := range []string{"a", "b", "c"} {
		if err := e.CreateComponent(context.Background(), core.Component{Key: key, Provider: "fake"}); err != nil {
			t.Fatalf("CreateComponent(%s): %v", key, err)
		}
	}

	checks, err := e.RunAll(context.Background())
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(checks) != 3 {
		t.Fatalf("RunAll returned %d checks, want 3", len(checks))
	}
	if st.recordCount() != 3 {
		t.Fatalf("recorded %d checks, want 3", st.recordCount())
	}
}

func TestEngineSnapshotWorstState(t *testing.T) {
	st := newFakeStore()
	e := New(st, st)

	ctx := context.Background()
	_ = st.RecordCheck(ctx, core.Check{Component: "a", State: core.StateOperational})
	_ = st.RecordCheck(ctx, core.Check{Component: "b", State: core.StateDegraded})
	_ = st.RecordCheck(ctx, core.Check{Component: "c", State: core.StateUnknown})

	h, err := e.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if h.State != core.StateDegraded {
		t.Fatalf("State = %v, want Degraded (worst real state)", h.State)
	}
	if len(h.Components) != 3 {
		t.Fatalf("Components = %d, want 3", len(h.Components))
	}
	if h.CheckedAt.IsZero() {
		t.Fatal("CheckedAt is zero")
	}
}

func TestEngineSnapshotUnknownDoesNotMaskDown(t *testing.T) {
	st := newFakeStore()
	e := New(st, st)

	ctx := context.Background()
	_ = st.RecordCheck(ctx, core.Check{Component: "a", State: core.StateUnknown})
	_ = st.RecordCheck(ctx, core.Check{Component: "b", State: core.StateDown})

	h, err := e.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if h.State != core.StateDown {
		t.Fatalf("State = %v, want Down", h.State)
	}
}

func TestEngineSnapshotEmpty(t *testing.T) {
	st := newFakeStore()
	e := New(st, st)

	h, err := e.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if h.State != core.StateUnknown {
		t.Fatalf("State = %v, want Unknown for empty snapshot", h.State)
	}
}
