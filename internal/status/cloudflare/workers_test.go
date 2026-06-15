package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moosequest/console/internal/core"
)

// fakeAPI returns a test server that responds with the given request/error
// counts, and records the last decoded request body + auth header.
func fakeAPI(t *testing.T, requests, errs int64) (*httptest.Server, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&cap.body)
		resp := fmt.Sprintf(`{"data":{"viewer":{"accounts":[{"workersInvocationsAdaptive":[{"sum":{"requests":%d,"errors":%d}}]}]}}}`, requests, errs)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

type capture struct {
	auth string
	body struct {
		Query     string            `json:"query"`
		Variables map[string]string `json:"variables"`
	}
}

func comp(cfg map[string]string) core.Component {
	return core.Component{Key: "api", Provider: "cloudflare-workers", Config: cfg}
}

func baseCfg() map[string]string {
	return map[string]string{"account_id": "acct123", "worker": "my-api", "api_token": "tok"}
}

func TestCheck_StateMapping(t *testing.T) {
	cases := []struct {
		name     string
		requests int64
		errs     int64
		cfg      map[string]string
		want     core.HealthState
	}{
		{"operational low errors", 1000, 5, nil, core.StateOperational}, // 0.5%
		{"degraded mid errors", 1000, 20, nil, core.StateDegraded},      // 2%
		{"down high errors", 1000, 80, nil, core.StateDown},             // 8%
		{"operational at boundary below 1%", 1000, 9, nil, core.StateOperational},
		{"degraded exactly 1%", 1000, 10, nil, core.StateDegraded},
		{"down exactly 5%", 1000, 50, nil, core.StateDown},
		// 7% is "down" under defaults but only "degraded" under these custom thresholds.
		{"custom thresholds", 1000, 70, map[string]string{"degraded_pct": "5", "down_pct": "10"}, core.StateDegraded},
		{"zero invocations -> unknown", 0, 0, nil, core.StateUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := fakeAPI(t, tc.requests, tc.errs)
			cfg := baseCfg()
			for k, v := range tc.cfg {
				cfg[k] = v
			}
			p := New(WithToken("tok"), WithEndpoint(srv.URL))
			got := p.Check(context.Background(), comp(cfg))
			if got.State != tc.want {
				t.Fatalf("state = %v, want %v (msg %q)", got.State, tc.want, got.Message)
			}
			if got.Component != "api" {
				t.Errorf("component = %q, want api", got.Component)
			}
			if tc.requests > 0 && got.Latency <= 0 {
				t.Errorf("expected latency to be populated")
			}
		})
	}
}

func TestCheck_RequestShapeAndAuth(t *testing.T) {
	srv, cap := fakeAPI(t, 100, 1)
	p := New(WithEndpoint(srv.URL))
	cfg := baseCfg()
	cfg["api_token"] = "" // force fallback to provider/default token
	p.Token = "provider-token"
	_ = p.Check(context.Background(), comp(cfg))

	if cap.auth != "Bearer provider-token" {
		t.Errorf("auth header = %q, want Bearer provider-token", cap.auth)
	}
	if cap.body.Variables["accountTag"] != "acct123" {
		t.Errorf("accountTag = %q", cap.body.Variables["accountTag"])
	}
	if cap.body.Variables["scriptName"] != "my-api" {
		t.Errorf("scriptName = %q", cap.body.Variables["scriptName"])
	}
	if cap.body.Variables["since"] == "" {
		t.Error("since variable not set")
	}
}

func TestCheck_TokenOverrideFromConfig(t *testing.T) {
	srv, cap := fakeAPI(t, 100, 0)
	p := New(WithToken("default"), WithEndpoint(srv.URL))
	cfg := baseCfg()
	cfg["api_token"] = "per-component"
	_ = p.Check(context.Background(), comp(cfg))
	if cap.auth != "Bearer per-component" {
		t.Errorf("auth = %q, want per-component to override default", cap.auth)
	}
}

func TestCheck_MissingConfig(t *testing.T) {
	p := New(WithEndpoint("http://unused.invalid"))
	for _, cfg := range []map[string]string{
		{"worker": "x", "api_token": "t"},     // no account
		{"account_id": "a", "api_token": "t"}, // no worker
		{"account_id": "a", "worker": "x"},    // no token (and provider has none)
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
		_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
	}))
	t.Cleanup(srv.Close)
	p := New(WithToken("tok"), WithEndpoint(srv.URL))
	got := p.Check(context.Background(), comp(baseCfg()))
	if got.State != core.StateDown {
		t.Fatalf("state = %v, want Down on api 500", got.State)
	}
}

func TestCheck_GraphQLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"unauthenticated"}]}`))
	}))
	t.Cleanup(srv.Close)
	p := New(WithToken("tok"), WithEndpoint(srv.URL))
	got := p.Check(context.Background(), comp(baseCfg()))
	if got.State != core.StateDown {
		t.Fatalf("state = %v, want Down on graphql error", got.State)
	}
}

func TestName(t *testing.T) {
	if New().Name() != "cloudflare-workers" {
		t.Fatalf("unexpected name")
	}
}
