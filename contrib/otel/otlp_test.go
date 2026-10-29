package otel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	collmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"

	"github.com/moosequest/console/internal/obs"
	"github.com/moosequest/console/internal/server"
)

// fakeOTLP is an in-process OTLP/HTTP receiver.
type fakeOTLP struct {
	mu      sync.Mutex
	traces  []*colltrace.ExportTraceServiceRequest
	metrics []*collmetrics.ExportMetricsServiceRequest
}

func (f *fakeOTLP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/traces":
		req := &colltrace.ExportTraceServiceRequest{}
		if proto.Unmarshal(body, req) == nil {
			f.traces = append(f.traces, req)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		b, _ := proto.Marshal(&colltrace.ExportTraceServiceResponse{})
		_, _ = w.Write(b)
	case "/v1/metrics":
		req := &collmetrics.ExportMetricsServiceRequest{}
		if proto.Unmarshal(body, req) == nil {
			f.metrics = append(f.metrics, req)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		b, _ := proto.Marshal(&collmetrics.ExportMetricsServiceResponse{})
		_, _ = w.Write(b)
	default:
		http.NotFound(w, r)
	}
}

func TestOTLPExport(t *testing.T) {
	fake := &fakeOTLP{}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	ctx := context.Background()
	rec, err := New(ctx, Config{Endpoint: srv.URL + "/", Version: "v9.9.9", MetricInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	rec.Counter("console_test_total", obs.L("kind", "a")).Add(3)
	rec.Gauge("console_test_gauge").Set(42)
	rec.Histogram("console_test_seconds").Observe(0.2)

	sctx := server.WithRequestID(ctx, "req-123")
	_, end := rec.StartSpan(sctx, "test.op", obs.L("op", "demo"))
	end(errors.New("failed"))

	if err := rec.Shutdown(ctx); err != nil { // flushes both pipelines
		t.Fatal(err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()

	// Traces.
	var gotSpan bool
	for _, req := range fake.traces {
		for _, rs := range req.ResourceSpans {
			svc := attrOf(rs.Resource.Attributes, "service.name")
			if svc != "console" {
				t.Errorf("service.name = %q", svc)
			}
			if v := attrOf(rs.Resource.Attributes, "service.version"); v != "v9.9.9" {
				t.Errorf("service.version = %q", v)
			}
			for _, ss := range rs.ScopeSpans {
				for _, sp := range ss.Spans {
					if sp.Name != "test.op" {
						continue
					}
					gotSpan = true
					if id := attrOf(sp.Attributes, RequestIDAttr); id != "req-123" {
						t.Errorf("request.id = %q", id)
					}
					if attrOf(sp.Attributes, "op") != "demo" {
						t.Error("missing op attribute")
					}
					if sp.Status.GetMessage() != "failed" {
						t.Errorf("span status = %v", sp.Status)
					}
				}
			}
		}
	}
	if !gotSpan {
		t.Fatalf("span not received (%d trace requests)", len(fake.traces))
	}

	// Metrics.
	seen := map[string]bool{}
	for _, req := range fake.metrics {
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					seen[m.Name] = true
					if m.Name == "console_test_total" {
						dp := m.GetSum().DataPoints[0]
						if dp.GetAsDouble() != 3 || attrOf(dp.Attributes, "kind") != "a" {
							t.Errorf("counter datapoint = %v", dp)
						}
					}
					if m.Name == "console_test_gauge" && m.GetGauge().DataPoints[0].GetAsDouble() != 42 {
						t.Errorf("gauge datapoint = %v", m.GetGauge().DataPoints[0])
					}
					if m.Name == "console_test_seconds" {
						dp := m.GetHistogram().DataPoints[0]
						if dp.Count != 1 || len(dp.ExplicitBounds) != len(obs.DefaultBuckets) {
							t.Errorf("histogram datapoint = %v", dp)
						}
					}
				}
			}
		}
	}
	for _, n := range []string{"console_test_total", "console_test_gauge", "console_test_seconds"} {
		if !seen[n] {
			t.Errorf("metric %s not received", n)
		}
	}
}

func TestNewRejectsBadEndpoint(t *testing.T) {
	for _, ep := range []string{"", "localhost:4318", "grpc://x"} {
		if _, err := New(context.Background(), Config{Endpoint: ep}); err == nil {
			t.Errorf("endpoint %q accepted", ep)
		}
	}
}

func attrOf(kvs []*commonpb.KeyValue, key string) string {
	for _, kv := range kvs {
		if kv.Key == key {
			return kv.Value.GetStringValue()
		}
	}
	return ""
}
