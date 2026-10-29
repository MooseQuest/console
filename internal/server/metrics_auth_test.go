package server

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

// TestMetricsListenerAuth covers the dedicated metrics listener handler: it
// enforces the read scope when an authenticator is set, serves only GET
// /metrics, and stays open when auth is disabled.
func TestMetricsListenerAuth(t *testing.T) {
	newH := func(withAuth bool) http.Handler {
		a := newMetricsApp(t, obs.NewMemory(), config.Config{Metrics: true, MetricsAddr: "127.0.0.1:0"})
		s := New(a).WithLogger(NewLogger(&bytes.Buffer{}, "text"))
		if withAuth {
			s = s.WithAuthenticator(testAuthn())
		}
		h := s.MetricsHandler()
		if h == nil {
			t.Fatal("MetricsHandler returned nil")
		}
		return h
	}

	cases := []struct {
		name, method, path, token string
		want                      int
	}{
		{"no token", "GET", "/metrics", "", 401},
		{"bad token", "GET", "/metrics", "nope", 401},
		{"eval token", "GET", "/metrics", tokEval, 403},
		{"read token", "GET", "/metrics", tokRead, 200},
		{"admin token", "GET", "/metrics", tokAdmin, 200},
		{"POST", "POST", "/metrics", tokAdmin, 405},
		{"api flags", "GET", "/api/flags", tokAdmin, 404},
		{"root", "GET", "/", tokAdmin, 404},
		{"healthz", "GET", "/healthz", tokAdmin, 404},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rr := call(newH(true), c.method, c.path, c.token); rr.Code != c.want {
				t.Errorf("%s %s = %d, want %d", c.method, c.path, rr.Code, c.want)
			}
		})
	}

	t.Run("auth disabled stays open", func(t *testing.T) {
		if rr := call(newH(false), "GET", "/metrics", ""); rr.Code != 200 {
			t.Errorf("GET /metrics = %d, want 200", rr.Code)
		}
	})

	t.Run("401 counted under GET /metrics", func(t *testing.T) {
		h := newH(true)
		if rr := call(h, "GET", "/metrics", ""); rr.Code != 401 {
			t.Fatalf("want 401, got %d", rr.Code)
		}
		body := call(h, "GET", "/metrics", tokRead).Body.String()
		want := `console_http_requests_total{code="401",method="GET",route="GET /metrics"} 1`
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q:\n%s", want, body)
		}
	})
}
