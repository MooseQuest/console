package flags

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/obs"
)

func obsFixture(t *testing.T, rec obs.Recorder) *Engine {
	t.Helper()
	e := New(newFakeStore())
	e.SetRecorder(rec)
	ctx := context.Background()
	for _, f := range []core.Flag{
		{Key: "off", Enabled: false, Scope: core.ScopeAll},
		{Key: "half", Enabled: true, Scope: core.ScopeAll, Rollout: 50},
		{Key: "all", Enabled: true, Scope: core.ScopeAll, Rollout: 100},
		{Key: "multi", Enabled: true, Scope: core.ScopeAll, Rollout: 100,
			Variants: []core.Variant{{Key: "a", Weight: 1}, {Key: "b", Weight: 3}}},
		{Key: "beta", Enabled: true, Scope: core.ScopeBeta, Rollout: 100},
	} {
		if err := e.Create(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// runAll evaluates a fixed grid and returns the serialized results.
func runAll(t *testing.T, e *Engine) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, k := range []string{"off", "half", "all", "multi", "beta", "missing"} {
		for i := 0; i < 200; i++ {
			ev, err := e.Evaluate(context.Background(), k, subject(fmt.Sprintf("u%d", i), map[string]string{"audience": "beta"}))
			b, _ := json.Marshal(ev)
			fmt.Fprintf(&buf, "%s|%v|%s\n", b, err, k)
		}
	}
	return buf.Bytes()
}

// Evaluation must stay deterministic and unaffected by instrumentation.
func TestEvaluateIdenticalWithNopAndMemory(t *testing.T) {
	nop := runAll(t, obsFixture(t, obs.Nop))
	mem := runAll(t, obsFixture(t, obs.NewMemory()))
	if !bytes.Equal(nop, mem) {
		t.Fatal("evaluation results differ between obs.Nop and obs.Memory")
	}
}

type errFlagStore struct{ *fakeStore }

func (errFlagStore) GetFlag(context.Context, string) (core.Flag, error) {
	return core.Flag{}, fmt.Errorf("boom")
}

func TestEvaluateMetrics(t *testing.T) {
	m := obs.NewMemory()
	e := obsFixture(t, m)
	ctx := context.Background()
	_, _ = e.Evaluate(ctx, "all", subject("secret-subject", nil))     // on
	_, _ = e.Evaluate(ctx, "off", subject("secret-subject", nil))     // off
	_, _ = e.Evaluate(ctx, "missing", subject("secret-subject", nil)) // not_found
	e.store = errFlagStore{e.store.(*fakeStore)}
	_, _ = e.Evaluate(ctx, "all", subject("secret-subject", nil)) // error

	var buf bytes.Buffer
	if err := m.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`console_flag_evaluations_total{result="on"} 1`,
		`console_flag_evaluations_total{result="off"} 1`,
		`console_flag_evaluations_total{result="not_found"} 1`,
		`console_flag_evaluations_total{result="error"} 1`,
		`console_flag_eval_duration_seconds_count 4`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret-subject") || strings.Contains(out, `="all"`) {
		t.Error("flag or subject key leaked into metrics")
	}
}
