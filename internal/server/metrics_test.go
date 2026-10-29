package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/flags"
	"github.com/moosequest/console/internal/obs"
	"github.com/moosequest/console/internal/status"
	"github.com/moosequest/console/internal/store/sqlite"
)

func newMetricsApp(t *testing.T, rec obs.Recorder, cfg config.Config) *app.App {
	t.Helper()
	st, err := sqlite.Open(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	a := &app.App{
		Obs: rec, Config: cfg, Store: st,
		Flags:  flags.New(st),
		Status: status.New(st, st, &status.HTTPProvider{}),
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func serve(t *testing.T, h http.Handler, method, path string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
	b, _ := io.ReadAll(rr.Body)
	return rr.Code, string(b)
}

func TestMetricsRouteLabels(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true})
	h := New(a).Handler()

	serve(t, h, "GET", "/api/flags/abc")
	serve(t, h, "GET", "/api/flags/def")
	serve(t, h, "GET", "/no/such/path/123")
	serve(t, h, "BREW", "/api/health")

	code, body := serve(t, h, "GET", "/metrics")
	if code != 200 {
		t.Fatalf("/metrics = %d", code)
	}
	for _, want := range []string{
		`console_http_requests_total{code="404",method="GET",route="GET /api/flags/{key}"} 2`,
		`console_http_requests_total{code="404",method="GET",route="unmatched"} 1`,
		`console_http_requests_total{code="405",method="OTHER",route="unmatched"} 1`,
		`console_http_request_duration_seconds_count{route="GET /api/flags/{key}"} 2`,
		`console_http_in_flight`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "abc") || strings.Contains(body, "/no/such") {
		t.Errorf("raw path leaked into labels:\n%s", body)
	}
}

func TestMetricsOffIs404(t *testing.T) {
	a := newMetricsApp(t, obs.Nop, config.Config{})
	s := New(a)
	if code, _ := serve(t, s.Handler(), "GET", "/metrics"); code != 404 {
		t.Fatalf("/metrics with metrics off = %d, want 404", code)
	}
	if s.MetricsHandler() != nil {
		t.Fatal("MetricsHandler should be nil when metrics are off")
	}
}

func TestMetricsSeparateListener(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true, MetricsAddr: "127.0.0.1:0"})
	s := New(a)
	if code, _ := serve(t, s.Handler(), "GET", "/metrics"); code != 404 {
		t.Fatalf("main listener /metrics = %d, want 404 when MetricsAddr set", code)
	}
	mh := s.MetricsHandler()
	if code, _ := serve(t, mh, "GET", "/metrics"); code != 200 {
		t.Fatalf("metrics listener /metrics = %d", code)
	}
	if code, _ := serve(t, mh, "GET", "/api/health"); code != 404 {
		t.Fatalf("metrics listener served non-metrics route: %d", code)
	}
}

func TestMetricsInFlightReturnsToZero(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true})
	h := New(a).Handler()
	serve(t, h, "GET", "/api/health")
	// Only the scrape itself is in flight while rendering.
	for i := 0; i < 2; i++ {
		_, body := serve(t, h, "GET", "/metrics")
		if !strings.Contains(body, "console_http_in_flight 1") {
			t.Errorf("scrape %d: want in_flight 1:\n%s", i, body)
		}
	}
}

// A panicking handler is recovered into a 500, and the metrics layer (outside
// recoverPanic) must still count it as a 500 against its route.
func TestMetricsCountsRecoveredPanicAs500(t *testing.T) {
	a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true})
	s := New(a)
	s.mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	h := s.Handler()

	if code, _ := serve(t, h, "GET", "/boom"); code != 500 {
		t.Fatalf("/boom = %d, want 500", code)
	}
	_, body := serve(t, h, "GET", "/metrics")
	want := `console_http_requests_total{code="500",method="GET",route="GET /boom"} 1`
	if !strings.Contains(body, want) {
		t.Errorf("metrics missing %q:\n%s", want, body)
	}
}
