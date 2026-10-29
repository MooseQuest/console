package otel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/moosequest/console/internal/obs"
)

// backend is a Recorder under test plus a way to read its metrics back in a
// normalized form: "name{a=1,b=2}" (labels sorted; histograms expand to
// name_count, name_sum and cumulative name_bucket with le) -> value.
type backend struct {
	rec  obs.Recorder
	read func(t *testing.T) map[string]float64
}

func key(name string, labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + labels[k]
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

func memoryBackend() backend {
	m := obs.NewMemory()
	return backend{rec: m, read: func(t *testing.T) map[string]float64 {
		var sb strings.Builder
		if err := m.WritePrometheus(&sb); err != nil {
			t.Fatal(err)
		}
		out := map[string]float64{}
		sc := bufio.NewScanner(strings.NewReader(sb.String()))
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "#") || line == "" {
				continue
			}
			sp := strings.LastIndexByte(line, ' ')
			series, val := line[:sp], line[sp+1:]
			v, err := strconv.ParseFloat(val, 64)
			if err != nil {
				t.Fatalf("parse %q: %v", line, err)
			}
			name, labels := series, map[string]string{}
			if i := strings.IndexByte(series, '{'); i >= 0 {
				name = series[:i]
				for _, kv := range strings.Split(strings.TrimSuffix(series[i+1:], "}"), ",") {
					if kv == "" {
						continue
					}
					k, rest, _ := strings.Cut(kv, "=")
					labels[k] = strings.Trim(rest, `"`)
				}
			}
			if le, ok := labels["le"]; ok {
				if le == "+Inf" {
					continue // equals _count; the OTel side has no explicit +Inf bound
				}
				if f, err := strconv.ParseFloat(le, 64); err == nil {
					labels["le"] = strconv.FormatFloat(f, 'g', -1, 64)
				}
			}
			out[key(name, labels)] = v
		}
		return out
	}}
}

func otelBackend() (backend, *tracetest.SpanRecorder) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	rec := NewWithProviders(tp, mp)
	return backend{rec: rec, read: func(t *testing.T) map[string]float64 {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatal(err)
		}
		out := map[string]float64{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				switch d := m.Data.(type) {
				case metricdata.Sum[float64]:
					for _, dp := range d.DataPoints {
						out[key(m.Name, attrMap(dp.Attributes.ToSlice()))] = dp.Value
					}
				case metricdata.Gauge[float64]:
					for _, dp := range d.DataPoints {
						out[key(m.Name, attrMap(dp.Attributes.ToSlice()))] = dp.Value
					}
				case metricdata.Histogram[float64]:
					for _, dp := range d.DataPoints {
						labels := attrMap(dp.Attributes.ToSlice())
						out[key(m.Name+"_count", labels)] = float64(dp.Count)
						out[key(m.Name+"_sum", labels)] = dp.Sum
						var cum uint64
						for i, b := range dp.Bounds {
							cum += dp.BucketCounts[i]
							l := map[string]string{"le": strconv.FormatFloat(b, 'g', -1, 64)}
							for k, v := range labels {
								l[k] = v
							}
							out[key(m.Name+"_bucket", l)] = float64(cum)
						}
					}
				}
			}
		}
		return out
	}}, sr
}

func TestConformance(t *testing.T) {
	ob, _ := otelBackend()
	backends := map[string]backend{"memory": memoryBackend(), "otel": ob}

	scenario := func(r obs.Recorder) {
		c := r.Counter("t_requests_total", obs.L("route", "GET /a"), obs.L("code", "2xx"))
		c.Inc()
		c.Add(2.5)
		c.Add(-3) // ignored
		c.Add(0)  // ignored
		// Same name+labels (different order) is the same series.
		r.Counter("t_requests_total", obs.L("code", "2xx"), obs.L("route", "GET /a")).Inc()
		r.Counter("t_requests_total", obs.L("route", "GET /b"), obs.L("code", "5xx")).Inc()
		r.Counter("t_plain_total").Add(7)

		g := r.Gauge("t_inflight", obs.L("pool", "x"))
		g.Set(10)
		g.Add(-2.5)
		g.Inc()
		g.Dec()
		g.Dec()
		r.Gauge("t_inflight", obs.L("pool", "x")).Add(0.5)

		h := r.Histogram("t_duration_seconds", obs.L("op", "get"))
		for _, v := range []float64{0.001, 0.005, 0.006, 0.3, 0.3, 10, 11} {
			h.Observe(v)
		}
	}

	results := map[string]map[string]float64{}
	for name, b := range backends {
		scenario(b.rec)
		results[name] = b.read(t)
	}
	if len(results["memory"]) == 0 {
		t.Fatal("memory backend produced no series")
	}
	if !approxEqual(results["memory"], results["otel"]) {
		t.Errorf("backends differ\nmemory: %v\notel:   %v", dump(results["memory"]), dump(results["otel"]))
	}

	// Spot-check a few absolute values so both backends can't agree on garbage.
	want := map[string]float64{
		key("t_requests_total", map[string]string{"route": "GET /a", "code": "2xx"}): 4.5,
		key("t_plain_total", nil):                                       7,
		key("t_inflight", map[string]string{"pool": "x"}):               7,
		key("t_duration_seconds_count", map[string]string{"op": "get"}): 7,
	}
	for k, v := range want {
		for name, res := range results {
			if res[k] != v {
				t.Errorf("%s: %s = %v, want %v", name, k, res[k], v)
			}
		}
	}
}

func TestConformanceKindClashAndDetached(t *testing.T) {
	ob, _ := otelBackend()
	for name, b := range map[string]backend{"memory": memoryBackend(), "otel": ob} {
		b.rec.Counter("t_clash").Inc()
		b.rec.Gauge("t_clash").Set(99) // wrong kind: works, but not exported
		if got := b.read(t)[key("t_clash", nil)]; got != 1 {
			t.Errorf("%s: t_clash = %v, want 1", name, got)
		}
	}
}

func TestSeriesCap(t *testing.T) {
	ob, _ := otelBackend()
	for i := 0; i < obs.MaxSeriesPerMetric+50; i++ {
		ob.rec.Counter("t_capped", obs.L("i", strconv.Itoa(i))).Inc()
	}
	n := 0
	for k := range ob.read(t) {
		if strings.HasPrefix(k, "t_capped{") {
			n++
		}
	}
	if n != obs.MaxSeriesPerMetric {
		t.Errorf("exported series = %d, want %d", n, obs.MaxSeriesPerMetric)
	}
}

func TestSpanEndWithError(t *testing.T) {
	ob, sr2 := otelBackend()
	rec := ob.rec

	ctx, end := rec.StartSpan(context.Background(), "parent", obs.L("op", "x"))
	_, endChild := rec.StartSpan(ctx, "child")
	endChild(errors.New("boom"))
	end(nil)

	spans := sr2.Ended()
	if len(spans) != 2 {
		t.Fatalf("ended spans = %d, want 2", len(spans))
	}
	child, parent := spans[0], spans[1]
	if child.Name() != "child" || parent.Name() != "parent" {
		t.Fatalf("names = %q, %q", child.Name(), parent.Name())
	}
	if child.Parent().SpanID() != parent.SpanContext().SpanID() || child.SpanContext().TraceID() != parent.SpanContext().TraceID() {
		t.Error("child span is not nested under parent")
	}
	if child.Status().Code.String() != "Error" || child.Status().Description != "boom" {
		t.Errorf("child status = %+v", child.Status())
	}
	if len(child.Events()) != 1 || child.Events()[0].Name != "exception" {
		t.Errorf("child events = %+v, want one exception event", child.Events())
	}
	if parent.Status().Code.String() == "Error" {
		t.Error("parent span should not be an error")
	}
	var op string
	for _, a := range parent.Attributes() {
		if string(a.Key) == "op" {
			op = a.Value.AsString()
		}
	}
	if op != "x" {
		t.Errorf("parent op attr = %q", op)
	}
}

func dump(m map[string]float64) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "\n  %s = %v", k, m[k])
	}
	return sb.String()
}

func attrMap(kvs []attribute.KeyValue) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

// approxEqual compares maps with a tiny tolerance: the backends sum floats in
// different orders, so sums may differ in the last bits.
func approxEqual(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || math.Abs(v-w) > 1e-9*math.Max(1, math.Abs(v)) {
			return false
		}
	}
	return true
}
