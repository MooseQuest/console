package obs_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/obs"
	"github.com/moosequest/console/internal/store/sqlite"
)

var dbCounter int64

func openSQLite(t *testing.T) *sqlite.Store {
	t.Helper()
	dsn := fmt.Sprintf("file:obsdb%d?mode=memory&cache=shared", atomic.AddInt64(&dbCounter, 1))
	s, err := sqlite.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func render(t *testing.T, m *obs.Memory) string {
	t.Helper()
	var b bytes.Buffer
	if err := m.WritePrometheus(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestWrapStoreSQLite(t *testing.T) {
	ctx := context.Background()
	m := obs.NewMemory()
	st := obs.WrapStore(openSQLite(t), m)

	if err := st.CreateFlag(ctx, core.Flag{Key: "a"}); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	if _, err := st.GetFlag(ctx, "a"); err != nil {
		t.Fatalf("GetFlag: %v", err)
	}
	// Sentinel errors pass through unchanged and are not counted as errors.
	if _, err := st.GetFlag(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := st.CreateFlag(ctx, core.Flag{Key: "a"}); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if err := st.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	out := render(t, m)
	for _, want := range []string{
		`console_store_op_duration_seconds_count{op="GetFlag"} 2`,
		`console_store_op_duration_seconds_count{op="CreateFlag"} 2`,
		`console_store_op_duration_seconds_count{op="Ping"} 1`,
		`console_store_errors_total{op="GetFlag"} 0`,
		`console_store_errors_total{op="CreateFlag"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWrapStoreCountsRealErrors(t *testing.T) {
	m := obs.NewMemory()
	inner := openSQLite(t)
	st := obs.WrapStore(inner, m)
	_ = inner.Close() // subsequent calls fail with a non-sentinel error

	if _, err := st.ListFlags(context.Background()); err == nil {
		t.Fatal("expected error from closed store")
	}
	out := render(t, m)
	if !strings.Contains(out, `console_store_errors_total{op="ListFlags"} 1`) {
		t.Errorf("error not counted:\n%s", out)
	}
}

func TestWrapStoreNilRecorder(t *testing.T) {
	st := obs.WrapStore(openSQLite(t), nil)
	if err := st.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type fakeProvider struct{ state core.HealthState }

func (fakeProvider) Name() string { return "fake" }
func (f fakeProvider) Check(_ context.Context, c core.Component) core.Check {
	return core.Check{Component: c.Key, State: f.state}
}

func TestWrapProvider(t *testing.T) {
	m := obs.NewMemory()
	ctx := context.Background()
	for _, state := range []core.HealthState{
		core.StateOperational,
		core.StateDown,
		core.HealthState(99), // out-of-range clamps to unknown
	} {
		p := obs.WrapProvider(fakeProvider{state}, m)
		if p.Name() != "fake" {
			t.Fatalf("Name = %q", p.Name())
		}
		got := p.Check(ctx, core.Component{Key: "component-id-1"})
		if got.State != state || got.Component != "component-id-1" {
			t.Fatalf("result altered: %+v", got)
		}
	}
	out := render(t, m)
	for _, want := range []string{
		`console_check_results_total{provider="fake",state="operational"} 1`,
		`console_check_results_total{provider="fake",state="down"} 1`,
		`console_check_results_total{provider="fake",state="unknown"} 1`,
		`console_check_duration_seconds_count{provider="fake"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "component-id-1") {
		t.Error("component ID leaked into labels")
	}
}
