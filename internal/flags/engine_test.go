package flags

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/moosequest/console/internal/core"
)

// fakeStore is an in-memory store.FlagStore for tests. It implements only the
// flag methods the engine uses, backed by a map.
type fakeStore struct {
	mu    sync.Mutex
	flags map[string]core.Flag
}

func newFakeStore() *fakeStore {
	return &fakeStore{flags: map[string]core.Flag{}}
}

func (s *fakeStore) CreateFlag(_ context.Context, f core.Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flags[f.Key]; ok {
		return core.ErrConflict
	}
	s.flags[f.Key] = f
	return nil
}

func (s *fakeStore) GetFlag(_ context.Context, key string) (core.Flag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flags[key]
	if !ok {
		return core.Flag{}, core.ErrNotFound
	}
	return f, nil
}

func (s *fakeStore) ListFlags(_ context.Context) ([]core.Flag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.Flag, 0, len(s.flags))
	for _, f := range s.flags {
		out = append(out, f)
	}
	return out, nil
}

func (s *fakeStore) UpdateFlag(_ context.Context, f core.Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flags[f.Key]; !ok {
		return core.ErrNotFound
	}
	s.flags[f.Key] = f
	return nil
}

func (s *fakeStore) DeleteFlag(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flags[key]; !ok {
		return core.ErrNotFound
	}
	delete(s.flags, key)
	return nil
}

func subject(key string, attrs map[string]string) core.Subject {
	return core.Subject{Key: key, Attributes: attrs}
}

func TestEvaluateNotFound(t *testing.T) {
	e := New(newFakeStore())
	_, err := e.Evaluate(context.Background(), "missing", subject("u1", nil))
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestEvaluateDisabled(t *testing.T) {
	s := newFakeStore()
	must(t, s.CreateFlag(context.Background(), core.Flag{Key: "f", Enabled: false, Scope: core.ScopeAll, Rollout: 100}))
	e := New(s)

	got, err := e.Evaluate(context.Background(), "f", subject("u1", nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Variant != "off" || got.Reason != "flag_disabled" {
		t.Fatalf("disabled flag: got %+v", got)
	}
}

func TestEvaluateOutOfScopeCohort(t *testing.T) {
	s := newFakeStore()
	must(t, s.CreateFlag(context.Background(), core.Flag{
		Key: "f", Enabled: true, Scope: core.ScopeCohort, Cohort: "enterprise", Rollout: 100,
	}))
	e := New(s)

	// Wrong cohort -> out of scope.
	got, err := e.Evaluate(context.Background(), "f", subject("u1", map[string]string{"cohort": "free"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Variant != "off" || got.Reason != "out_of_scope" {
		t.Fatalf("cohort mismatch: got %+v", got)
	}

	// Matching cohort -> in scope, rolled in.
	got, err = e.Evaluate(context.Background(), "f", subject("u1", map[string]string{"cohort": "enterprise"}))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Variant != "on" || got.Reason != "rollout_included" {
		t.Fatalf("cohort match: got %+v", got)
	}
}

func TestEvaluateBetaScope(t *testing.T) {
	s := newFakeStore()
	must(t, s.CreateFlag(context.Background(), core.Flag{Key: "f", Enabled: true, Scope: core.ScopeBeta, Rollout: 100}))
	e := New(s)

	cases := []struct {
		name    string
		attrs   map[string]string
		inScope bool
	}{
		{"audience=beta", map[string]string{"audience": "beta"}, true},
		{"beta=true", map[string]string{"beta": "true"}, true},
		{"no marker", map[string]string{"audience": "ga"}, false},
		{"nil attrs", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Evaluate(context.Background(), "f", subject("u1", tc.attrs))
			if err != nil {
				t.Fatal(err)
			}
			if tc.inScope && got.Reason != "rollout_included" {
				t.Fatalf("want in scope, got %+v", got)
			}
			if !tc.inScope && got.Reason != "out_of_scope" {
				t.Fatalf("want out of scope, got %+v", got)
			}
		})
	}
}

func TestEvaluateRolloutDeterminism(t *testing.T) {
	s := newFakeStore()
	must(t, s.CreateFlag(context.Background(), core.Flag{Key: "f", Enabled: true, Scope: core.ScopeAll, Rollout: 50}))
	e := New(s)

	subj := subject("stable-user", nil)
	first, err := e.Evaluate(context.Background(), "f", subj)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		got, err := e.Evaluate(context.Background(), "f", subj)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Fatalf("non-deterministic: call %d got %+v, want %+v", i, got, first)
		}
	}
}

func TestEvaluateRolloutBoundaries(t *testing.T) {
	s := newFakeStore()
	must(t, s.CreateFlag(context.Background(), core.Flag{Key: "zero", Enabled: true, Scope: core.ScopeAll, Rollout: 0}))
	must(t, s.CreateFlag(context.Background(), core.Flag{Key: "full", Enabled: true, Scope: core.ScopeAll, Rollout: 100}))
	e := New(s)

	for i := 0; i < 500; i++ {
		subj := subject(fmt.Sprintf("u%d", i), nil)

		got, err := e.Evaluate(context.Background(), "zero", subj)
		if err != nil {
			t.Fatal(err)
		}
		if got.Enabled {
			t.Fatalf("0%% rollout included %s: %+v", subj.Key, got)
		}

		got, err = e.Evaluate(context.Background(), "full", subj)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Enabled {
			t.Fatalf("100%% rollout excluded %s: %+v", subj.Key, got)
		}
	}
}

func TestEvaluateRolloutDistribution(t *testing.T) {
	const (
		rollout = 30
		n       = 20000
		tol     = 3 // percentage points
	)
	s := newFakeStore()
	must(t, s.CreateFlag(context.Background(), core.Flag{Key: "f", Enabled: true, Scope: core.ScopeAll, Rollout: rollout}))
	e := New(s)

	included := 0
	for i := 0; i < n; i++ {
		got, err := e.Evaluate(context.Background(), "f", subject(fmt.Sprintf("user-%d", i), nil))
		if err != nil {
			t.Fatal(err)
		}
		if got.Enabled {
			included++
		}
	}
	pct := float64(included) / float64(n) * 100
	if pct < rollout-tol || pct > rollout+tol {
		t.Fatalf("rollout distribution off: got %.2f%%, want ~%d%% (+/-%d)", pct, rollout, tol)
	}
}

func TestEvaluateBooleanOn(t *testing.T) {
	s := newFakeStore()
	must(t, s.CreateFlag(context.Background(), core.Flag{Key: "f", Enabled: true, Scope: core.ScopeAll, Rollout: 100}))
	e := New(s)

	got, err := e.Evaluate(context.Background(), "f", subject("u1", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Variant != "on" || got.Value != "" || got.Reason != "rollout_included" {
		t.Fatalf("boolean on: got %+v", got)
	}
}

func TestEvaluateMultivariateDistribution(t *testing.T) {
	const (
		n   = 30000
		tol = 3.0 // percentage points
	)
	s := newFakeStore()
	must(t, s.CreateFlag(context.Background(), core.Flag{
		Key: "f", Enabled: true, Scope: core.ScopeAll, Rollout: 100,
		Variants: []core.Variant{
			{Key: "control", Value: "c", Weight: 60},
			{Key: "test", Value: "t", Weight: 30},
			{Key: "holdout", Value: "h", Weight: 10},
		},
	}))
	e := New(s)

	counts := map[string]int{}
	for i := 0; i < n; i++ {
		got, err := e.Evaluate(context.Background(), "f", subject(fmt.Sprintf("user-%d", i), nil))
		if err != nil {
			t.Fatal(err)
		}
		if !got.Enabled || got.Reason != "variant" {
			t.Fatalf("multivariate: unexpected %+v", got)
		}
		counts[got.Variant]++
	}

	want := map[string]float64{"control": 60, "test": 30, "holdout": 10}
	for key, wantPct := range want {
		gotPct := float64(counts[key]) / float64(n) * 100
		if gotPct < wantPct-tol || gotPct > wantPct+tol {
			t.Fatalf("variant %q: got %.2f%%, want ~%.0f%% (+/-%.0f)", key, gotPct, wantPct, tol)
		}
	}
}

func TestEvaluateMultivariateDeterminism(t *testing.T) {
	s := newFakeStore()
	must(t, s.CreateFlag(context.Background(), core.Flag{
		Key: "f", Enabled: true, Scope: core.ScopeAll, Rollout: 100,
		Variants: []core.Variant{
			{Key: "a", Value: "1", Weight: 50},
			{Key: "b", Value: "2", Weight: 50},
		},
	}))
	e := New(s)

	subj := subject("steady", nil)
	first, err := e.Evaluate(context.Background(), "f", subj)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		got, err := e.Evaluate(context.Background(), "f", subj)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Fatalf("variant non-deterministic: got %+v, want %+v", got, first)
		}
	}
}

func TestCRUDPassthrough(t *testing.T) {
	ctx := context.Background()
	e := New(newFakeStore())

	f := core.Flag{Key: "f", Enabled: true, Scope: core.ScopeAll, Rollout: 100}
	must(t, e.Create(ctx, f))

	got, err := e.Get(ctx, "f")
	if err != nil || got.Key != "f" {
		t.Fatalf("get: %+v, %v", got, err)
	}

	list, err := e.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v, %v", list, err)
	}

	f.Rollout = 50
	must(t, e.Update(ctx, f))
	got, _ = e.Get(ctx, "f")
	if got.Rollout != 50 {
		t.Fatalf("update not persisted: %+v", got)
	}

	must(t, e.Delete(ctx, "f"))
	if _, err := e.Get(ctx, "f"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("after delete want ErrNotFound, got %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
