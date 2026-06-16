package heroku

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/core"
)

// fakeAPI returns a test server that responds with a dynos array built from the
// given states, and records the last request path + auth/accept headers.
func fakeAPI(t *testing.T, states []string) (*httptest.Server, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.auth = r.Header.Get("Authorization")
		cap.accept = r.Header.Get("Accept")
		cap.path = r.URL.Path
		parts := make([]string, 0, len(states))
		for _, s := range states {
			parts = append(parts, `{"state":"`+s+`"}`)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

type capture struct {
	auth   string
	accept string
	path   string
}

func comp(cfg map[string]string) core.Component {
	return core.Component{Key: "api", Provider: "heroku", Config: cfg}
}

func baseCfg() map[string]string {
	return map[string]string{"app": "my-app", "api_token": "tok"}
}

func TestCheck_StateMapping(t *testing.T) {
	cases := []struct {
		name   string
		states []string
		want   core.HealthState
	}{
		{"all up -> operational", []string{"up", "up", "up"}, core.StateOperational},
		{"single up -> operational", []string{"up"}, core.StateOperational},
		{"some up some not -> degraded", []string{"up", "crashed"}, core.StateDegraded},
		{"up and starting -> degraded", []string{"up", "starting", "up"}, core.StateDegraded},
		{"none up -> down", []string{"crashed", "down"}, core.StateDown},
		{"all crashed -> down", []string{"crashed"}, core.StateDown},
		{"zero dynos -> unknown", []string{}, core.StateUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := fakeAPI(t, tc.states)
			p := New(WithToken("tok"), WithBaseURL(srv.URL))
			got := p.Check(context.Background(), comp(baseCfg()))
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
	srv, cap := fakeAPI(t, []string{"up"})
	p := New(WithBaseURL(srv.URL))
	cfg := baseCfg()
	cfg["api_token"] = "" // force fallback to provider/default token
	p.Token = "provider-token"
	_ = p.Check(context.Background(), comp(cfg))

	if cap.auth != "Bearer provider-token" {
		t.Errorf("auth header = %q, want Bearer provider-token", cap.auth)
	}
	if cap.accept != "application/vnd.heroku+json; version=3" {
		t.Errorf("accept header = %q", cap.accept)
	}
	if cap.path != "/apps/my-app/dynos" {
		t.Errorf("path = %q, want /apps/my-app/dynos", cap.path)
	}
}

func TestCheck_TokenOverrideFromConfig(t *testing.T) {
	srv, cap := fakeAPI(t, []string{"up"})
	p := New(WithToken("default"), WithBaseURL(srv.URL))
	cfg := baseCfg()
	cfg["api_token"] = "per-component"
	_ = p.Check(context.Background(), comp(cfg))
	if cap.auth != "Bearer per-component" {
		t.Errorf("auth = %q, want per-component to override default", cap.auth)
	}
}

func TestCheck_MissingConfig(t *testing.T) {
	p := New(WithBaseURL("http://unused.invalid"))
	for _, cfg := range []map[string]string{
		{"api_token": "t"}, // no app
		{"app": "a"},       // no token (and provider has none)
	} {
		got := p.Check(context.Background(), comp(cfg))
		if got.State != core.StateUnknown {
			t.Errorf("cfg %v: state = %v, want Unknown", cfg, got.State)
		}
	}
}

func TestCheck_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"id":"server_error","message":"boom"}`))
	}))
	t.Cleanup(srv.Close)
	p := New(WithToken("tok"), WithBaseURL(srv.URL))
	got := p.Check(context.Background(), comp(baseCfg()))
	if got.State != core.StateDown {
		t.Fatalf("state = %v, want Down on api 500", got.State)
	}
}

func TestName(t *testing.T) {
	if New().Name() != "heroku" {
		t.Fatalf("unexpected name")
	}
}
