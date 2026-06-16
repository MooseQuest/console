package sentry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/core"
)

// fakeAPI returns a test server that responds with an issues array of the given
// length, and records the last request path/query + auth header.
func fakeAPI(t *testing.T, count int) (*httptest.Server, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.auth = r.Header.Get("Authorization")
		cap.path = r.URL.Path
		cap.query = r.URL.RawQuery
		parts := make([]string, 0, count)
		for i := 0; i < count; i++ {
			parts = append(parts, "{}")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

type capture struct {
	auth  string
	path  string
	query string
}

func comp(cfg map[string]string) core.Component {
	return core.Component{Key: "api", Provider: "sentry", Config: cfg}
}

func baseCfg() map[string]string {
	return map[string]string{"org": "acme", "project": "backend", "auth_token": "tok"}
}

func TestCheck_StateMapping(t *testing.T) {
	cases := []struct {
		name  string
		count int
		cfg   map[string]string
		want  core.HealthState
	}{
		{"zero -> operational", 0, nil, core.StateOperational},
		{"one -> degraded (default)", 1, nil, core.StateDegraded},
		{"five -> degraded (default)", 5, nil, core.StateDegraded},
		{"ten -> down (default)", 10, nil, core.StateDown},
		{"twenty -> down (default)", 20, nil, core.StateDown},
		// custom thresholds: degraded at 5, down at 50.
		{"three under custom degraded -> operational", 3, map[string]string{"degraded_count": "5", "down_count": "50"}, core.StateOperational},
		{"ten under custom down -> degraded", 10, map[string]string{"degraded_count": "5", "down_count": "50"}, core.StateDegraded},
		{"sixty over custom down -> down", 60, map[string]string{"degraded_count": "5", "down_count": "50"}, core.StateDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := fakeAPI(t, tc.count)
			cfg := baseCfg()
			for k, v := range tc.cfg {
				cfg[k] = v
			}
			p := New(WithToken("tok"), WithBaseURL(srv.URL))
			got := p.Check(context.Background(), comp(cfg))
			if got.State != tc.want {
				t.Fatalf("state = %v, want %v (msg %q)", got.State, tc.want, got.Message)
			}
			if got.Component != "api" {
				t.Errorf("component = %q, want api", got.Component)
			}
			if got.Latency <= 0 {
				t.Errorf("expected latency to be populated")
			}
		})
	}
}

func TestCheck_RequestShapeAndAuth(t *testing.T) {
	srv, cap := fakeAPI(t, 0)
	p := New(WithBaseURL(srv.URL))
	cfg := baseCfg()
	cfg["auth_token"] = "" // force fallback to provider/default token
	p.Token = "provider-token"
	_ = p.Check(context.Background(), comp(cfg))

	if cap.auth != "Bearer provider-token" {
		t.Errorf("auth header = %q, want Bearer provider-token", cap.auth)
	}
	if cap.path != "/api/0/projects/acme/backend/issues/" {
		t.Errorf("path = %q, want /api/0/projects/acme/backend/issues/", cap.path)
	}
	if !strings.Contains(cap.query, "is:unresolved") || !strings.Contains(cap.query, "limit=100") {
		t.Errorf("query = %q, want is:unresolved and limit=100", cap.query)
	}
}

func TestCheck_CountSaturatesAtPageLimit(t *testing.T) {
	srv, _ := fakeAPI(t, issuePageLimit) // exactly one full page
	p := New(WithToken("tok"), WithBaseURL(srv.URL))
	got := p.Check(context.Background(), comp(baseCfg()))
	if got.State != core.StateDown {
		t.Fatalf("state = %v, want Down at page limit", got.State)
	}
	if !strings.Contains(got.Message, "100+") {
		t.Errorf("message = %q, want it to signal saturation (100+)", got.Message)
	}
}

func TestCheck_TokenOverrideFromConfig(t *testing.T) {
	srv, cap := fakeAPI(t, 0)
	p := New(WithToken("default"), WithBaseURL(srv.URL))
	cfg := baseCfg()
	cfg["auth_token"] = "per-component"
	_ = p.Check(context.Background(), comp(cfg))
	if cap.auth != "Bearer per-component" {
		t.Errorf("auth = %q, want per-component to override default", cap.auth)
	}
}

func TestCheck_MissingConfig(t *testing.T) {
	p := New(WithBaseURL("http://unused.invalid"))
	for _, cfg := range []map[string]string{
		{"project": "p", "auth_token": "t"}, // no org
		{"org": "o", "auth_token": "t"},     // no project
		{"org": "o", "project": "p"},        // no token (and provider has none)
	} {
		got := p.Check(context.Background(), comp(cfg))
		if got.State != core.StateUnknown {
			t.Errorf("cfg %v: state = %v, want Unknown", cfg, got.State)
		}
	}
}

func TestCheck_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"Invalid token"}`))
	}))
	t.Cleanup(srv.Close)
	p := New(WithToken("tok"), WithBaseURL(srv.URL))
	got := p.Check(context.Background(), comp(baseCfg()))
	if got.State != core.StateDown {
		t.Fatalf("state = %v, want Down on api 401", got.State)
	}
}

func TestName(t *testing.T) {
	if New().Name() != "sentry" {
		t.Fatalf("unexpected name")
	}
}
