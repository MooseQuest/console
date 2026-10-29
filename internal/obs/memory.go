package obs

import (
	"context"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// DefaultBuckets are the fixed histogram upper bounds, in seconds, used by
// [Memory]: 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10. An
// implicit +Inf bucket is always added. Treat as read-only.
var DefaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// MaxSeriesPerMetric bounds the number of label combinations [Memory] retains
// per metric name. Combinations beyond it get working but unexported
// instruments, so a cardinality bug cannot exhaust memory.
const MaxSeriesPerMetric = 1000

type kind int

const (
	kindCounter kind = iota
	kindGauge
	kindHistogram
)

func (k kind) String() string {
	switch k {
	case kindGauge:
		return "gauge"
	case kindHistogram:
		return "histogram"
	}
	return "counter"
}

// Memory is an in-process Recorder that keeps all series in memory and renders
// them in the Prometheus text exposition format via [Memory.WritePrometheus].
// Spans are no-ops that preserve the context. Create one with [NewMemory].
type Memory struct {
	mu         sync.Mutex
	families   map[string]*family
	collectors []func(Recorder)
}

type family struct {
	kind   kind
	series map[string]*series
}

type series struct {
	labels []Label // sorted by name
	// bits holds the counter / gauge value as float64 bits.
	bits atomic.Uint64
	// histogram state, guarded by hmu.
	hmu    sync.Mutex
	counts []uint64 // per-bucket (non-cumulative), len(DefaultBuckets)+1
	sum    float64
	count  uint64
}

// NewMemory returns an empty in-memory registry.
func NewMemory() *Memory {
	return &Memory{families: make(map[string]*family)}
}

// AddCollector registers fn to run at the start of every
// [Memory.WritePrometheus] call, before rendering. Collectors refresh
// scrape-time values (for example runtime stats set on gauges) using the
// Recorder passed in. fn must not call WritePrometheus.
func (m *Memory) AddCollector(fn func(Recorder)) {
	m.mu.Lock()
	m.collectors = append(m.collectors, fn)
	m.mu.Unlock()
}

// Counter implements [Recorder].
func (m *Memory) Counter(name string, labels ...Label) Counter {
	return (*counter)(m.get(name, kindCounter, labels))
}

// Gauge implements [Recorder].
func (m *Memory) Gauge(name string, labels ...Label) Gauge {
	return (*gauge)(m.get(name, kindGauge, labels))
}

// Histogram implements [Recorder].
func (m *Memory) Histogram(name string, labels ...Label) Histogram {
	return (*histogram)(m.get(name, kindHistogram, labels))
}

// StartSpan implements [Recorder]. It is a no-op: the returned context is ctx
// itself and the end func does nothing.
func (m *Memory) StartSpan(ctx context.Context, _ string, _ ...Label) (context.Context, func(error)) {
	return ctx, nopEnd
}

// get returns the series for name/labels, creating it on first use. If the
// name is already registered as a different kind, or the family is at its
// series cap, a detached series is returned so callers never see nil.
func (m *Memory) get(name string, k kind, labels []Label) *series {
	sorted := sortedLabels(labels)
	key := labelKey(sorted)

	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.families[name]
	if !ok {
		f = &family{kind: k, series: make(map[string]*series)}
		m.families[name] = f
	}
	if f.kind != k {
		return newSeries(sorted, k)
	}
	if s, ok := f.series[key]; ok {
		return s
	}
	s := newSeries(sorted, k)
	if len(f.series) < MaxSeriesPerMetric {
		f.series[key] = s
	}
	return s
}

func newSeries(labels []Label, k kind) *series {
	s := &series{labels: labels}
	if k == kindHistogram {
		s.counts = make([]uint64, len(DefaultBuckets)+1)
	}
	return s
}

func sortedLabels(labels []Label) []Label {
	if len(labels) == 0 {
		return nil
	}
	out := make([]Label, len(labels))
	copy(out, labels)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func labelKey(labels []Label) string {
	var b strings.Builder
	for _, l := range labels {
		b.WriteString(l.Name)
		b.WriteByte(0)
		b.WriteString(l.Value)
		b.WriteByte(0)
	}
	return b.String()
}

func (s *series) load() float64 { return math.Float64frombits(s.bits.Load()) }

func (s *series) add(v float64) {
	for {
		old := s.bits.Load()
		if s.bits.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+v)) {
			return
		}
	}
}

type counter series

func (c *counter) Inc() { (*series)(c).add(1) }
func (c *counter) Add(v float64) {
	if v > 0 {
		(*series)(c).add(v)
	}
}

type gauge series

func (g *gauge) Set(v float64) { g.bits.Store(math.Float64bits(v)) }
func (g *gauge) Add(v float64) { (*series)(g).add(v) }
func (g *gauge) Inc()          { (*series)(g).add(1) }
func (g *gauge) Dec()          { (*series)(g).add(-1) }

type histogram series

func (h *histogram) Observe(v float64) {
	i := sort.SearchFloat64s(DefaultBuckets, v) // first bucket with bound >= v
	h.hmu.Lock()
	h.counts[i]++
	h.sum += v
	h.count++
	h.hmu.Unlock()
}

// WritePrometheus runs the registered collectors, then writes every series in
// the Prometheus text exposition format (version 0.0.4). Output is
// deterministic: families sorted by name, series sorted by labels.
func (m *Memory) WritePrometheus(w io.Writer) error {
	m.mu.Lock()
	collectors := make([]func(Recorder), len(m.collectors))
	copy(collectors, m.collectors)
	m.mu.Unlock()
	for _, fn := range collectors {
		fn(m)
	}

	type famSnap struct {
		name   string
		kind   kind
		series []*series
	}
	m.mu.Lock()
	snaps := make([]famSnap, 0, len(m.families))
	for name, f := range m.families {
		fs := famSnap{name: name, kind: f.kind, series: make([]*series, 0, len(f.series))}
		keys := make([]string, 0, len(f.series))
		for k := range f.series {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fs.series = append(fs.series, f.series[k])
		}
		snaps = append(snaps, fs)
	}
	m.mu.Unlock()
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].name < snaps[j].name })

	var b strings.Builder
	for _, f := range snaps {
		b.WriteString("# TYPE " + f.name + " " + f.kind.String() + "\n")
		for _, s := range f.series {
			if f.kind == kindHistogram {
				writeHistogram(&b, f.name, s)
				continue
			}
			b.WriteString(f.name + formatLabels(s.labels, "") + " " + formatFloat(s.load()) + "\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeHistogram(b *strings.Builder, name string, s *series) {
	s.hmu.Lock()
	counts := append([]uint64(nil), s.counts...)
	sum, count := s.sum, s.count
	s.hmu.Unlock()

	var cum uint64
	for i, bound := range DefaultBuckets {
		cum += counts[i]
		b.WriteString(name + "_bucket" + formatLabels(s.labels, formatFloat(bound)) + " " + strconv.FormatUint(cum, 10) + "\n")
	}
	b.WriteString(name + "_bucket" + formatLabels(s.labels, "+Inf") + " " + strconv.FormatUint(count, 10) + "\n")
	b.WriteString(name + "_sum" + formatLabels(s.labels, "") + " " + formatFloat(sum) + "\n")
	b.WriteString(name + "_count" + formatLabels(s.labels, "") + " " + strconv.FormatUint(count, 10) + "\n")
}

// formatLabels renders {a="1",b="2"}; a non-empty le appends the bucket label.
func formatLabels(labels []Label, le string) string {
	if len(labels) == 0 && le == "" {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, l := range labels {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(l.Name + `="` + escapeValue(l.Value) + `"`)
	}
	if le != "" {
		if len(labels) > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`le="` + le + `"`)
	}
	b.WriteByte('}')
	return b.String()
}

var valueEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)

func escapeValue(v string) string { return valueEscaper.Replace(v) }

func formatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
