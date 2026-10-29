package flags

import (
	"context"
	"os"
	"testing"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/obs"
)

// Performance budget guards for flag-evaluation instrumentation
// (docs/design/apm.md section 4.6): at most ~1 us and zero allocations added
// per evaluation, measured as the delta between obs.Nop and obs.Memory.

// maxAddedNs is the latency budget for instrumentation, in ns/op.
const maxAddedNs = 1000

// benchEval measures Engine.Evaluate against a rollout flag with rec installed.
func benchEval(b *testing.B, rec obs.Recorder) {
	e := obsFixture(b, rec)
	ctx := context.Background()
	subj := core.Subject{Key: "user-1", Attributes: map[string]string{"audience": "beta"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Evaluate(ctx, "half", subj)
	}
}

func BenchmarkEvaluateNop(b *testing.B)    { benchEval(b, obs.Nop) }
func BenchmarkEvaluateMemory(b *testing.B) { benchEval(b, obs.NewMemory()) }

// TestInstrumentationAddsNoAllocs fails if recording with obs.Memory allocates
// more per evaluation than obs.Nop on the same path.
func TestInstrumentationAddsNoAllocs(t *testing.T) {
	ctx := context.Background()
	subj := core.Subject{Key: "user-1", Attributes: map[string]string{"audience": "beta"}}
	allocs := func(rec obs.Recorder) float64 {
		e := obsFixture(t, rec)
		return testing.AllocsPerRun(1000, func() { _, _ = e.Evaluate(ctx, "half", subj) })
	}
	nop, mem := allocs(obs.Nop), allocs(obs.NewMemory())
	if mem > nop {
		t.Fatalf("instrumentation adds %.1f allocs/op (Nop %.1f, Memory %.1f), budget is 0", mem-nop, nop, mem)
	}
}

// TestInstrumentationLatencyBudget compares Nop and Memory in the same run and
// fails if the added time exceeds maxAddedNs. Wall-clock timing is noisy on
// shared machines, so it only runs when CONSOLE_PERF=1 (set by CI).
func TestInstrumentationLatencyBudget(t *testing.T) {
	if os.Getenv("CONSOLE_PERF") != "1" {
		t.Skip("set CONSOLE_PERF=1 to run the latency budget check")
	}
	// Best of several runs to damp scheduler noise.
	best := func(rec obs.Recorder) float64 {
		min := -1.0
		for i := 0; i < 3; i++ {
			r := testing.Benchmark(func(b *testing.B) { benchEval(b, rec) })
			if ns := float64(r.NsPerOp()); min < 0 || ns < min {
				min = ns
			}
		}
		return min
	}
	nop, mem := best(obs.Nop), best(obs.NewMemory())
	delta := mem - nop
	t.Logf("eval: Nop %.0f ns/op, Memory %.0f ns/op, added %.0f ns/op (budget %d)", nop, mem, delta, maxAddedNs)
	if delta > maxAddedNs {
		t.Fatalf("instrumentation adds %.0f ns/op, budget is %d", delta, maxAddedNs)
	}
}
