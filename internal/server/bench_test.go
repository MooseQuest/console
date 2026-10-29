package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

// benchHandler measures a request through the full middleware chain with rec
// installed; obs.Nop is the "instrumentation off" baseline.
func benchHandler(b *testing.B, rec obs.Recorder) {
	a := newMetricsApp(b, rec, config.Config{})
	h := New(a).Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkHandlerNop(b *testing.B)    { benchHandler(b, obs.Nop) }
func BenchmarkHandlerMemory(b *testing.B) { benchHandler(b, obs.NewMemory()) }
