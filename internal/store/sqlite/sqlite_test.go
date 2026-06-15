package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moosequest/console/internal/core"
)

var memCounter int64

// newStore opens a fresh, isolated in-memory database for a single test. Each
// store gets a uniquely named shared-cache DSN so tests do not share state.
func newStore(t *testing.T) *Store {
	t.Helper()
	dsn := fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", atomic.AddInt64(&memCounter, 1))
	s, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPing(t *testing.T) {
	s := newStore(t)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestFlagRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		flag core.Flag
	}{
		{
			name: "boolean flag, no variants",
			flag: core.Flag{
				Key:         "new-ui",
				Description: "the new dashboard",
				Enabled:     true,
				Scope:       core.ScopeBeta,
				Rollout:     50,
			},
		},
		{
			name: "multivariate flag with variants",
			flag: core.Flag{
				Key:     "checkout-color",
				Enabled: true,
				Scope:   core.ScopeExperiment,
				Rollout: 100,
				Variants: []core.Variant{
					{Key: "green", Value: "#0f0", Weight: 1},
					{Key: "blue", Value: "#00f", Weight: 2},
				},
				Experiment: "exp-42",
			},
		},
		{
			name: "cohort flag",
			flag: core.Flag{
				Key:    "vip",
				Scope:  core.ScopeCohort,
				Cohort: "whales",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			ctx := context.Background()
			if err := s.CreateFlag(ctx, tc.flag); err != nil {
				t.Fatalf("CreateFlag: %v", err)
			}
			got, err := s.GetFlag(ctx, tc.flag.Key)
			if err != nil {
				t.Fatalf("GetFlag: %v", err)
			}
			if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
				t.Errorf("timestamps not set: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
			}
			// Compare ignoring timestamps (set by store).
			want := tc.flag
			want.CreatedAt, want.UpdatedAt = got.CreatedAt, got.UpdatedAt
			if !reflect.DeepEqual(got, want) {
				t.Errorf("roundtrip mismatch:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestFlagNotFound(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.GetFlag(ctx, "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetFlag: want ErrNotFound, got %v", err)
	}
	if err := s.UpdateFlag(ctx, core.Flag{Key: "nope"}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("UpdateFlag: want ErrNotFound, got %v", err)
	}
	if err := s.DeleteFlag(ctx, "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("DeleteFlag: want ErrNotFound, got %v", err)
	}
}

func TestFlagConflict(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	f := core.Flag{Key: "dup"}
	if err := s.CreateFlag(ctx, f); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	if err := s.CreateFlag(ctx, f); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("CreateFlag dup: want ErrConflict, got %v", err)
	}
}

func TestFlagUpdateAndList(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.CreateFlag(ctx, core.Flag{Key: "a", CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatalf("CreateFlag a: %v", err)
	}
	if err := s.CreateFlag(ctx, core.Flag{Key: "b"}); err != nil {
		t.Fatalf("CreateFlag b: %v", err)
	}

	upd := core.Flag{
		Key:         "a",
		Description: "updated",
		Enabled:     true,
		Rollout:     25,
		Variants:    []core.Variant{{Key: "x", Value: "1", Weight: 1}},
	}
	if err := s.UpdateFlag(ctx, upd); err != nil {
		t.Fatalf("UpdateFlag: %v", err)
	}
	got, err := s.GetFlag(ctx, "a")
	if err != nil {
		t.Fatalf("GetFlag: %v", err)
	}
	if got.Description != "updated" || !got.Enabled || got.Rollout != 25 || len(got.Variants) != 1 {
		t.Errorf("update not applied: %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt not preserved: got %v want %v", got.CreatedAt, created)
	}

	flags, err := s.ListFlags(ctx)
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if len(flags) != 2 || flags[0].Key != "a" || flags[1].Key != "b" {
		t.Errorf("ListFlags ordering: %+v", flags)
	}

	if err := s.DeleteFlag(ctx, "a"); err != nil {
		t.Fatalf("DeleteFlag: %v", err)
	}
	flags, _ = s.ListFlags(ctx)
	if len(flags) != 1 || flags[0].Key != "b" {
		t.Errorf("after delete: %+v", flags)
	}
}

func TestComponentRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		comp core.Component
	}{
		{
			name: "with config",
			comp: core.Component{
				Key:         "api",
				Name:        "API",
				Description: "the public API",
				Provider:    "http",
				Config:      map[string]string{"url": "https://example.com", "method": "GET"},
			},
		},
		{
			name: "no config",
			comp: core.Component{
				Key:      "worker",
				Name:     "Worker",
				Provider: "tcp",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			ctx := context.Background()
			if err := s.CreateComponent(ctx, tc.comp); err != nil {
				t.Fatalf("CreateComponent: %v", err)
			}
			got, err := s.GetComponent(ctx, tc.comp.Key)
			if err != nil {
				t.Fatalf("GetComponent: %v", err)
			}
			if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
				t.Errorf("timestamps not set")
			}
			want := tc.comp
			want.CreatedAt, want.UpdatedAt = got.CreatedAt, got.UpdatedAt
			if !reflect.DeepEqual(got, want) {
				t.Errorf("roundtrip mismatch:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestComponentNotFoundAndConflict(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.GetComponent(ctx, "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetComponent: want ErrNotFound, got %v", err)
	}
	if err := s.UpdateComponent(ctx, core.Component{Key: "nope"}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("UpdateComponent: want ErrNotFound, got %v", err)
	}
	if err := s.DeleteComponent(ctx, "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("DeleteComponent: want ErrNotFound, got %v", err)
	}
	c := core.Component{Key: "dup"}
	if err := s.CreateComponent(ctx, c); err != nil {
		t.Fatalf("CreateComponent: %v", err)
	}
	if err := s.CreateComponent(ctx, c); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("CreateComponent dup: want ErrConflict, got %v", err)
	}
}

func TestComponentUpdateAndList(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.CreateComponent(ctx, core.Component{Key: "a", Name: "A"}); err != nil {
		t.Fatalf("CreateComponent a: %v", err)
	}
	if err := s.CreateComponent(ctx, core.Component{Key: "b", Name: "B"}); err != nil {
		t.Fatalf("CreateComponent b: %v", err)
	}
	if err := s.UpdateComponent(ctx, core.Component{Key: "a", Name: "A2", Config: map[string]string{"k": "v"}}); err != nil {
		t.Fatalf("UpdateComponent: %v", err)
	}
	got, _ := s.GetComponent(ctx, "a")
	if got.Name != "A2" || got.Config["k"] != "v" {
		t.Errorf("update not applied: %+v", got)
	}
	comps, err := s.ListComponents(ctx)
	if err != nil {
		t.Fatalf("ListComponents: %v", err)
	}
	if len(comps) != 2 || comps[0].Key != "a" || comps[1].Key != "b" {
		t.Errorf("ListComponents ordering: %+v", comps)
	}
	if err := s.DeleteComponent(ctx, "b"); err != nil {
		t.Fatalf("DeleteComponent: %v", err)
	}
	comps, _ = s.ListComponents(ctx)
	if len(comps) != 1 || comps[0].Key != "a" {
		t.Errorf("after delete: %+v", comps)
	}
}

func TestChecks(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, k := range []string{"api", "db"} {
		if err := s.CreateComponent(ctx, core.Component{Key: k}); err != nil {
			t.Fatalf("CreateComponent %s: %v", k, err)
		}
	}

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	checks := []core.Check{
		{Component: "api", State: core.StateOperational, Message: "ok", Latency: 5 * time.Millisecond, CheckedAt: base},
		{Component: "api", State: core.StateDegraded, Message: "slow", Latency: 200 * time.Millisecond, CheckedAt: base.Add(time.Minute)},
		{Component: "db", State: core.StateDown, Message: "gone", CheckedAt: base.Add(30 * time.Second)},
	}
	for _, c := range checks {
		if err := s.RecordCheck(ctx, c); err != nil {
			t.Fatalf("RecordCheck: %v", err)
		}
	}

	// LatestCheck for api should be the degraded one.
	got, err := s.LatestCheck(ctx, "api")
	if err != nil {
		t.Fatalf("LatestCheck: %v", err)
	}
	if got.State != core.StateDegraded || got.Message != "slow" || got.Latency != 200*time.Millisecond {
		t.Errorf("LatestCheck api: %+v", got)
	}
	if !got.CheckedAt.Equal(base.Add(time.Minute)) {
		t.Errorf("LatestCheck api CheckedAt: got %v", got.CheckedAt)
	}

	// LatestCheck for a component with no checks.
	if err := s.CreateComponent(ctx, core.Component{Key: "empty"}); err != nil {
		t.Fatalf("CreateComponent empty: %v", err)
	}
	if _, err := s.LatestCheck(ctx, "empty"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("LatestCheck empty: want ErrNotFound, got %v", err)
	}

	// LatestChecks should return one row per component (api degraded, db down).
	all, err := s.LatestChecks(ctx)
	if err != nil {
		t.Fatalf("LatestChecks: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("LatestChecks: want 2 rows, got %d: %+v", len(all), all)
	}
	byComp := map[string]core.Check{}
	for _, c := range all {
		byComp[c.Component] = c
	}
	if byComp["api"].State != core.StateDegraded {
		t.Errorf("LatestChecks api: %+v", byComp["api"])
	}
	if byComp["db"].State != core.StateDown {
		t.Errorf("LatestChecks db: %+v", byComp["db"])
	}
}
