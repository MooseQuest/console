package server

import (
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/moosequest/console/internal/obs"
)

// unmatchedRoute is the single route label used for requests that matched no
// registered pattern (404s, 405s), keeping label cardinality bounded.
const unmatchedRoute = "unmatched"

// promWriter is implemented by recorders that can render the Prometheus text
// exposition format (obs.Memory). obs.Nop does not, so /metrics is not served.
type promWriter interface {
	WritePrometheus(w io.Writer) error
}

// metricsHandler serves the Prometheus text exposition of rec, or nil when the
// recorder cannot render it (metrics disabled). It is a plain http.Handler so
// it can be wrapped (e.g. by an auth scope check) or mounted on its own
// listener.
func metricsHandler(rec obs.Recorder) http.Handler {
	pw, ok := rec.(promWriter)
	if !ok {
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = pw.WritePrometheus(w)
	})
}

// MetricsHandler returns a handler serving only GET /metrics, for the optional
// dedicated metrics listener (CONSOLE_METRICS_ADDR). It returns nil when
// metrics are disabled.
func (s *Server) MetricsHandler() http.Handler {
	h := metricsHandler(s.app.Obs)
	if h == nil {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", h)
	return mux
}

// httpMetrics records RED metrics for every request. Instruments are resolved
// once per distinct label set and cached, not looked up on every request.
type httpMetrics struct {
	rec      obs.Recorder
	inFlight obs.Gauge
	counters sync.Map // counterKey -> obs.Counter
	hists    sync.Map // route string -> obs.Histogram
}

type counterKey struct {
	route, method string
	code          int
}

func newHTTPMetrics(rec obs.Recorder) *httpMetrics {
	if rec == nil { // hand-built Apps (tests) may leave Obs unset
		rec = obs.Nop
	}
	return &httpMetrics{rec: rec, inFlight: rec.Gauge("console_http_in_flight")}
}

// boundedMethod maps the client-supplied method onto a fixed set so a hostile
// client cannot mint unbounded series.
func boundedMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return m
	}
	return "OTHER"
}

func (m *httpMetrics) counter(k counterKey) obs.Counter {
	if c, ok := m.counters.Load(k); ok {
		return c.(obs.Counter)
	}
	c := m.rec.Counter("console_http_requests_total",
		obs.L("route", k.route), obs.L("method", k.method), obs.L("code", strconv.Itoa(k.code)))
	actual, _ := m.counters.LoadOrStore(k, c)
	return actual.(obs.Counter)
}

func (m *httpMetrics) hist(route string) obs.Histogram {
	if h, ok := m.hists.Load(route); ok {
		return h.(obs.Histogram)
	}
	h := m.rec.Histogram("console_http_request_duration_seconds", obs.L("route", route))
	actual, _ := m.hists.LoadOrStore(route, h)
	return actual.(obs.Histogram)
}

// metricsWriter captures the response status. Unwrap exposes the underlying
// writer to http.ResponseController (Flush, etc).
type metricsWriter struct {
	http.ResponseWriter
	status int
}

func (w *metricsWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *metricsWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *metricsWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// middleware wraps next, recording request count, duration and in-flight. The
// route label is read from r.Pattern after the mux has routed (ServeMux sets it
// on the request it was given), never from the raw path.
func (m *httpMetrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		m.inFlight.Inc()
		mw := &metricsWriter{ResponseWriter: w}
		defer func() {
			m.inFlight.Dec()
			route := r.Pattern
			if route == "" {
				route = unmatchedRoute
			}
			code := mw.status
			if code == 0 {
				code = http.StatusOK
			}
			m.counter(counterKey{route, boundedMethod(r.Method), code}).Inc()
			m.hist(route).Observe(time.Since(start).Seconds())
		}()
		next.ServeHTTP(mw, r)
	})
}
