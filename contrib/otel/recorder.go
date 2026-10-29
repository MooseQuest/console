// Package otel adapts Console's [obs.Recorder] seam to the OpenTelemetry SDK,
// exporting metrics and traces over OTLP/HTTP. It lives in its own Go module
// (contrib/otel) so the root module and the default console binary never depend
// on go.opentelemetry.io (see docs/design/apm.md 4.1.1).
package otel

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/moosequest/console/internal/obs"
	"github.com/moosequest/console/internal/server"
)

// scopeName is the OTel instrumentation scope for everything Console records.
const scopeName = "github.com/moosequest/console"

// RequestIDAttr is the span attribute carrying the Console request ID (the
// X-Request-Id value stored in the request context by the server).
const RequestIDAttr = "request.id"

type kind int

const (
	kindCounter kind = iota
	kindGauge
	kindHistogram
)

// Recorder implements [obs.Recorder] on top of the OpenTelemetry SDK. Create
// one with [New] (OTLP export) or [NewWithProviders] (caller-supplied
// providers, used by tests).
type Recorder struct {
	meter  metric.Meter
	tracer oteltrace.Tracer

	tp *sdktrace.TracerProvider
	mp *sdkmetric.MeterProvider

	mu       sync.Mutex
	families map[string]*family
}

type family struct {
	kind   kind
	ctr    metric.Float64Counter
	gauge  metric.Float64Gauge
	hist   metric.Float64Histogram
	series map[string]*series
}

// series is one name+label-set; it backs counter, gauge and histogram handles.
type series struct {
	attrs metric.MeasurementOption
	f     *family
	bits  atomic.Uint64 // gauge value
}

var _ obs.Recorder = (*Recorder)(nil)

// NewWithProviders builds a Recorder over the given providers. Shutdown shuts
// both providers down.
func NewWithProviders(tp *sdktrace.TracerProvider, mp *sdkmetric.MeterProvider) *Recorder {
	return &Recorder{
		meter:    mp.Meter(scopeName),
		tracer:   tp.Tracer(scopeName),
		tp:       tp,
		mp:       mp,
		families: make(map[string]*family),
	}
}

// Shutdown flushes and stops the exporters. Call it once before process exit.
func (r *Recorder) Shutdown(ctx context.Context) error {
	err := r.tp.Shutdown(ctx)
	if merr := r.mp.Shutdown(ctx); err == nil {
		err = merr
	}
	return err
}

// ForceFlush exports pending spans and metrics immediately.
func (r *Recorder) ForceFlush(ctx context.Context) error {
	err := r.tp.ForceFlush(ctx)
	if merr := r.mp.ForceFlush(ctx); err == nil {
		err = merr
	}
	return err
}

func toAttrs(labels []obs.Label) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, len(labels))
	for i, l := range labels {
		out[i] = attribute.String(l.Name, l.Value)
	}
	return out
}

func seriesKey(labels []obs.Label) string {
	s := make([]obs.Label, len(labels))
	copy(s, labels)
	sort.SliceStable(s, func(i, j int) bool { return s[i].Name < s[j].Name })
	var b strings.Builder
	for _, l := range s {
		b.WriteString(l.Name)
		b.WriteByte(0)
		b.WriteString(l.Value)
		b.WriteByte(0)
	}
	return b.String()
}

// get returns the series for name/labels, creating it on first use. A kind
// clash or a family at obs.MaxSeriesPerMetric yields a detached series (works,
// is never exported), mirroring obs.Memory.
func (r *Recorder) get(name string, k kind, labels []obs.Label) *series {
	key := seriesKey(labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.families[name]
	if !ok {
		f = &family{kind: k, series: make(map[string]*series)}
		// An invalid instrument name leaves the instrument nil, so records
		// are dropped; the family is kept so creation is not retried.
		switch k {
		case kindCounter:
			f.ctr, _ = r.meter.Float64Counter(name)
		case kindGauge:
			f.gauge, _ = r.meter.Float64Gauge(name)
		case kindHistogram:
			f.hist, _ = r.meter.Float64Histogram(name,
				metric.WithExplicitBucketBoundaries(obs.DefaultBuckets...))
		}
		r.families[name] = f
	}
	if f.kind != k {
		return &series{f: &family{kind: k}}
	}
	if s, ok := f.series[key]; ok {
		return s
	}
	s := &series{f: f, attrs: metric.WithAttributes(toAttrs(labels)...)}
	if len(f.series) < obs.MaxSeriesPerMetric {
		f.series[key] = s
	} else {
		s.f = &family{kind: k} // detached: instruments nil, records dropped
	}
	return s
}

type counter series

func (c *counter) Inc() { c.Add(1) }
func (c *counter) Add(v float64) {
	if v > 0 && c.f.ctr != nil {
		c.f.ctr.Add(context.Background(), v, c.attrs)
	}
}

type gauge series

func (g *gauge) add(v float64) {
	s := (*series)(g)
	for {
		old := s.bits.Load()
		nv := math.Float64frombits(old) + v
		if s.bits.CompareAndSwap(old, math.Float64bits(nv)) {
			g.record(nv)
			return
		}
	}
}

func (g *gauge) record(v float64) {
	if g.f.gauge != nil {
		g.f.gauge.Record(context.Background(), v, g.attrs)
	}
}

func (g *gauge) Set(v float64) {
	(*series)(g).bits.Store(math.Float64bits(v))
	g.record(v)
}
func (g *gauge) Add(v float64) { g.add(v) }
func (g *gauge) Inc()          { g.add(1) }
func (g *gauge) Dec()          { g.add(-1) }

type histogram series

func (h *histogram) Observe(v float64) {
	if h.f.hist != nil {
		h.f.hist.Record(context.Background(), v, h.attrs)
	}
}

// Counter implements [obs.Recorder].
func (r *Recorder) Counter(name string, labels ...obs.Label) obs.Counter {
	return (*counter)(r.get(name, kindCounter, labels))
}

// Gauge implements [obs.Recorder]. OTel's synchronous gauge only records
// absolute values, so the adapter tracks the running value per series and
// records it after every change.
func (r *Recorder) Gauge(name string, labels ...obs.Label) obs.Gauge {
	return (*gauge)(r.get(name, kindGauge, labels))
}

// Histogram implements [obs.Recorder], using obs.DefaultBuckets as explicit
// bucket boundaries so output matches obs.Memory.
func (r *Recorder) Histogram(name string, labels ...obs.Label) obs.Histogram {
	return (*histogram)(r.get(name, kindHistogram, labels))
}

// StartSpan implements [obs.Recorder]. The returned context carries the new
// span (so children nest and trace context propagates). The Console request ID
// in ctx, when present, is attached as [RequestIDAttr]. The end func records a
// non-nil err on the span and marks it failed.
func (r *Recorder) StartSpan(ctx context.Context, name string, labels ...obs.Label) (context.Context, func(error)) {
	attrs := toAttrs(labels)
	if id := server.RequestID(ctx); id != "" {
		attrs = append(attrs, attribute.String(RequestIDAttr, id))
	}
	ctx, span := r.tracer.Start(ctx, name, oteltrace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}
