package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/moosequest/console/internal/core"
)

// setup opens the store against CONSOLE_TEST_POSTGRES_DSN, runs migrations
// (via Open), and truncates the tables for test isolation. It skips the test
// when no DSN is configured so `go test` stays green without a database.
func setup(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set CONSOLE_TEST_POSTGRES_DSN to run Postgres store tests")
	}
	ctx := context.Background()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if _, err := st.db.ExecContext(ctx, `TRUNCATE checks, components, flags RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return st, ctx
}

func TestFlagRoundtrip(t *testing.T) {
	st, ctx := setup(t)

	in := core.Flag{
		Key:         "f1",
		Description: "first flag",
		Enabled:     true,
		Scope:       core.ScopeExperiment,
		Rollout:     42,
		Variants: []core.Variant{
			{Key: "on", Value: "true", Weight: 70},
			{Key: "off", Value: "false", Weight: 30},
		},
		Cohort:     "beta-cohort",
		Experiment: "exp-1",
	}
	if err := st.CreateFlag(ctx, in); err != nil {
		t.Fatalf("create flag: %v", err)
	}

	got, err := st.GetFlag(ctx, "f1")
	if err != nil {
		t.Fatalf("get flag: %v", err)
	}
	if got.Key != in.Key || got.Description != in.Description || got.Enabled != in.Enabled ||
		got.Scope != in.Scope || got.Rollout != in.Rollout || got.Cohort != in.Cohort ||
		got.Experiment != in.Experiment {
		t.Fatalf("flag mismatch: got %+v want %+v", got, in)
	}
	if len(got.Variants) != 2 || got.Variants[0] != in.Variants[0] || got.Variants[1] != in.Variants[1] {
		t.Fatalf("variants mismatch: got %+v", got.Variants)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: %+v", got)
	}
	if got.CreatedAt.Location() != time.UTC {
		t.Fatalf("created_at not UTC: %v", got.CreatedAt.Location())
	}
}

func TestFlagNotFound(t *testing.T) {
	st, ctx := setup(t)

	if _, err := st.GetFlag(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("get: want ErrNotFound, got %v", err)
	}
	if err := st.UpdateFlag(ctx, core.Flag{Key: "missing"}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("update: want ErrNotFound, got %v", err)
	}
	if err := st.DeleteFlag(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("delete: want ErrNotFound, got %v", err)
	}
}

func TestFlagConflict(t *testing.T) {
	st, ctx := setup(t)

	f := core.Flag{Key: "dup"}
	if err := st.CreateFlag(ctx, f); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.CreateFlag(ctx, f); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("create dup: want ErrConflict, got %v", err)
	}
}

func TestFlagUpdate(t *testing.T) {
	st, ctx := setup(t)

	created := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Microsecond)
	in := core.Flag{Key: "u1", Description: "old", CreatedAt: created, UpdatedAt: created}
	if err := st.CreateFlag(ctx, in); err != nil {
		t.Fatalf("create: %v", err)
	}

	in.Description = "new"
	in.Enabled = true
	in.UpdatedAt = time.Time{} // force now()
	if err := st.UpdateFlag(ctx, in); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := st.GetFlag(ctx, "u1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Description != "new" || !got.Enabled {
		t.Fatalf("update not applied: %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("created_at not preserved: got %v want %v", got.CreatedAt, created)
	}
	if !got.UpdatedAt.After(created) {
		t.Fatalf("updated_at not advanced: got %v", got.UpdatedAt)
	}
}

func TestFlagListAndDelete(t *testing.T) {
	st, ctx := setup(t)

	for _, k := range []string{"b", "a", "c"} {
		if err := st.CreateFlag(ctx, core.Flag{Key: k}); err != nil {
			t.Fatalf("create %s: %v", k, err)
		}
	}
	flags, err := st.ListFlags(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(flags) != 3 || flags[0].Key != "a" || flags[1].Key != "b" || flags[2].Key != "c" {
		t.Fatalf("list order wrong: %+v", flags)
	}

	if err := st.DeleteFlag(ctx, "b"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	flags, _ = st.ListFlags(ctx)
	if len(flags) != 2 {
		t.Fatalf("after delete want 2, got %d", len(flags))
	}
}

func TestComponentRoundtrip(t *testing.T) {
	st, ctx := setup(t)

	in := core.Component{
		Key:         "c1",
		Name:        "API",
		Description: "the api",
		Provider:    "http",
		Config:      map[string]string{"url": "https://example.com", "timeout": "5s"},
	}
	if err := st.CreateComponent(ctx, in); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := st.GetComponent(ctx, "c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != in.Name || got.Description != in.Description || got.Provider != in.Provider {
		t.Fatalf("component mismatch: %+v", got)
	}
	if len(got.Config) != 2 || got.Config["url"] != "https://example.com" || got.Config["timeout"] != "5s" {
		t.Fatalf("config mismatch: %+v", got.Config)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: %+v", got)
	}
}

func TestComponentNotFoundAndConflict(t *testing.T) {
	st, ctx := setup(t)

	if _, err := st.GetComponent(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("get: want ErrNotFound, got %v", err)
	}
	if err := st.UpdateComponent(ctx, core.Component{Key: "missing"}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("update: want ErrNotFound, got %v", err)
	}
	if err := st.DeleteComponent(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("delete: want ErrNotFound, got %v", err)
	}

	c := core.Component{Key: "dup"}
	if err := st.CreateComponent(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.CreateComponent(ctx, c); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("create dup: want ErrConflict, got %v", err)
	}
}

func TestComponentUpdate(t *testing.T) {
	st, ctx := setup(t)

	created := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Microsecond)
	in := core.Component{Key: "cu", Name: "old", CreatedAt: created, UpdatedAt: created}
	if err := st.CreateComponent(ctx, in); err != nil {
		t.Fatalf("create: %v", err)
	}
	in.Name = "new"
	in.Config = map[string]string{"k": "v"}
	in.UpdatedAt = time.Time{}
	if err := st.UpdateComponent(ctx, in); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := st.GetComponent(ctx, "cu")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "new" || got.Config["k"] != "v" {
		t.Fatalf("update not applied: %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("created_at not preserved: %v", got.CreatedAt)
	}
	if !got.UpdatedAt.After(created) {
		t.Fatalf("updated_at not advanced: %v", got.UpdatedAt)
	}
}

func TestComponentList(t *testing.T) {
	st, ctx := setup(t)

	for _, k := range []string{"z", "x", "y"} {
		if err := st.CreateComponent(ctx, core.Component{Key: k}); err != nil {
			t.Fatalf("create %s: %v", k, err)
		}
	}
	comps, err := st.ListComponents(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(comps) != 3 || comps[0].Key != "x" || comps[1].Key != "y" || comps[2].Key != "z" {
		t.Fatalf("list order wrong: %+v", comps)
	}
}

func TestChecks(t *testing.T) {
	st, ctx := setup(t)

	// Checks reference components via FK; create the components first.
	for _, k := range []string{"api", "db"} {
		if err := st.CreateComponent(ctx, core.Component{Key: k}); err != nil {
			t.Fatalf("create component %s: %v", k, err)
		}
	}

	// No checks yet.
	if _, err := st.LatestCheck(ctx, "api"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("latest with no checks: want ErrNotFound, got %v", err)
	}

	base := time.Now().UTC().Truncate(time.Microsecond)
	checks := []core.Check{
		{Component: "api", State: core.StateOperational, Message: "ok", Latency: 10 * time.Millisecond, CheckedAt: base.Add(-2 * time.Minute)},
		{Component: "api", State: core.StateDegraded, Message: "slow", Latency: 200 * time.Millisecond, CheckedAt: base.Add(-1 * time.Minute)},
		{Component: "api", State: core.StateDown, Message: "down", Latency: 0, CheckedAt: base},
		{Component: "db", State: core.StateOperational, Message: "fine", Latency: 5 * time.Millisecond, CheckedAt: base.Add(-30 * time.Second)},
	}
	for _, c := range checks {
		if err := st.RecordCheck(ctx, c); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	latest, err := st.LatestCheck(ctx, "api")
	if err != nil {
		t.Fatalf("latest check: %v", err)
	}
	if latest.State != core.StateDown || latest.Message != "down" {
		t.Fatalf("latest api wrong: %+v", latest)
	}
	if !latest.CheckedAt.Equal(base) {
		t.Fatalf("latest checked_at mismatch: got %v want %v", latest.CheckedAt, base)
	}

	all, err := st.LatestChecks(ctx)
	if err != nil {
		t.Fatalf("latest checks: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 latest checks, got %d: %+v", len(all), all)
	}
	byComp := map[string]core.Check{}
	for _, c := range all {
		byComp[c.Component] = c
	}
	if byComp["api"].State != core.StateDown {
		t.Fatalf("api latest wrong: %+v", byComp["api"])
	}
	if byComp["db"].State != core.StateOperational || byComp["db"].Latency != 5*time.Millisecond {
		t.Fatalf("db latest wrong: %+v", byComp["db"])
	}
}

func TestPing(t *testing.T) {
	st, ctx := setup(t)
	if err := st.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
