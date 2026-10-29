package obs

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestNopAllocFree(t *testing.T) {
	r := Nop
	ctx := context.Background()
	cases := map[string]func(){
		"Counter":   func() { r.Counter("c").Inc(); r.Counter("c").Add(2) },
		"Histogram": func() { r.Histogram("h").Observe(1) },
		"Gauge":     func() { r.Gauge("g").Set(1); r.Gauge("g").Inc() },
		"StartSpan": func() { _, end := r.StartSpan(ctx, "s"); end(nil) },
	}
	for name, fn := range cases {
		if n := testing.AllocsPerRun(100, fn); n != 0 {
			t.Errorf("%s: %v allocs, want 0", name, n)
		}
	}
}

func TestMemorySpanPreservesContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "v")
	got, end := NewMemory().StartSpan(ctx, "x")
	end(nil)
	if got != ctx {
		t.Fatal("StartSpan must return the same ctx")
	}
}

func TestPrometheusExact(t *testing.T) {
	m := NewMemory()
	m.Counter("req_total", L("route", "GET /a"), L("code", "200")).Inc()
	m.Counter("req_total", L("code", "200"), L("route", "GET /a")).Add(2) // label order irrelevant
	m.Counter("req_total", L("code", "500"), L("route", `we"ird\`)).Inc()
	m.Counter("req_total").Add(-5) // ignored
	m.Gauge("inflight").Set(3)
	m.Gauge("inflight").Dec()
	h := m.Histogram("dur_seconds", L("route", "r"))
	h.Observe(0.003)
	h.Observe(0.05)
	h.Observe(20)
	m.AddCollector(func(r Recorder) { r.Gauge("collected").Set(7) })

	var b strings.Builder
	if err := m.WritePrometheus(&b); err != nil {
		t.Fatal(err)
	}
	want := `# TYPE collected gauge
collected 7
# TYPE dur_seconds histogram
dur_seconds_bucket{route="r",le="0.005"} 1
dur_seconds_bucket{route="r",le="0.01"} 1
dur_seconds_bucket{route="r",le="0.025"} 1
dur_seconds_bucket{route="r",le="0.05"} 2
dur_seconds_bucket{route="r",le="0.1"} 2
dur_seconds_bucket{route="r",le="0.25"} 2
dur_seconds_bucket{route="r",le="0.5"} 2
dur_seconds_bucket{route="r",le="1"} 2
dur_seconds_bucket{route="r",le="2.5"} 2
dur_seconds_bucket{route="r",le="5"} 2
dur_seconds_bucket{route="r",le="10"} 2
dur_seconds_bucket{route="r",le="+Inf"} 3
dur_seconds_sum{route="r"} 20.053
dur_seconds_count{route="r"} 3
# TYPE inflight gauge
inflight 2
# TYPE req_total counter
req_total 0
req_total{code="200",route="GET /a"} 3
req_total{code="500",route="we\"ird\\"} 1
`
	if got := b.String(); got != want {
		t.Fatalf("output mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestKindConflictAndCap(t *testing.T) {
	m := NewMemory()
	m.Counter("x").Inc()
	m.Gauge("x").Set(9) // wrong kind: detached, must not panic or export
	for i := 0; i < MaxSeriesPerMetric+10; i++ {
		m.Counter("y", L("i", strconv.Itoa(i))).Inc()
	}
	m.mu.Lock()
	n := len(m.families["y"].series)
	k := m.families["x"].kind
	m.mu.Unlock()
	if n != MaxSeriesPerMetric || k != kindCounter {
		t.Fatalf("series=%d kind=%v", n, k)
	}
}

func TestMemoryConcurrent(t *testing.T) {
	m := NewMemory()
	m.AddCollector(func(r Recorder) { r.Gauge("scrapes").Inc() })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				m.Counter("c", L("k", "v")).Inc()
				m.Gauge("g").Add(1)
				m.Histogram("h").Observe(float64(j) / 100)
				if j%50 == 0 {
					var b strings.Builder
					_ = m.WritePrometheus(&b)
				}
			}
		}()
	}
	wg.Wait()
	var b strings.Builder
	_ = m.WritePrometheus(&b)
	out := b.String()
	if !strings.Contains(out, `c{k="v"} 4000`) || !strings.Contains(out, "h_count 4000") {
		t.Fatalf("lost updates:\n%s", out)
	}
}
