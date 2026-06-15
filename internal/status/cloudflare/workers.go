// Package cloudflare provides Console status providers backed by Cloudflare's
// APIs. WorkersProvider reports the health of a Cloudflare Worker from its
// recent invocation analytics (request and error counts) via the Cloudflare
// GraphQL Analytics API.
//
// It satisfies the status.Provider interface structurally (Name + Check), so it
// does not import the status package and adds no coupling beyond core.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/moosequest/console/internal/core"
)

// defaultEndpoint is the Cloudflare GraphQL Analytics API.
const defaultEndpoint = "https://api.cloudflare.com/client/v4/graphql"

// Default thresholds and window used when a component does not override them.
const (
	defaultWindow      = 15 * time.Minute
	defaultDegradedPct = 1.0 // error rate at/above this is degraded
	defaultDownPct     = 5.0 // error rate at/above this is down
	defaultTimeout     = 10 * time.Second
)

// WorkersProvider checks a Cloudflare Worker's health from its invocation
// analytics. The error rate over a trailing window maps to a health state.
//
// It reads its target from core.Component.Config:
//
//	account_id    required; the Cloudflare account tag
//	worker        required; the Worker script name (alias: "script")
//	api_token     optional; overrides the provider's default token
//	window        optional; trailing window as a Go duration, default "15m"
//	degraded_pct  optional; error-rate %% at/above which state is degraded (default 1)
//	down_pct      optional; error-rate %% at/above which state is down (default 5)
//	timeout       optional; API call timeout as a Go duration, default "10s"
//
// State mapping (error rate = errors / requests over the window):
//
//	rate < degraded_pct                 -> StateOperational
//	degraded_pct <= rate < down_pct     -> StateDegraded
//	rate >= down_pct                    -> StateDown
//	API/network failure or GraphQL error -> StateDown
//	no token / account_id / worker      -> StateUnknown (configuration gap)
//	zero invocations in the window      -> StateUnknown (idle, not unhealthy)
type WorkersProvider struct {
	// Token is the default Cloudflare API token, used when a component does not
	// set "api_token". Typically sourced from the CLOUDFLARE_API_TOKEN env var.
	Token string
	// Endpoint is the GraphQL API URL; defaults to Cloudflare's. Overridable for
	// testing or a self-hosted gateway.
	Endpoint string
	// HTTP issues the API request. If nil, a client with a sane timeout is used.
	HTTP *http.Client
}

// Option configures a WorkersProvider.
type Option func(*WorkersProvider)

// WithToken sets the default API token.
func WithToken(t string) Option { return func(p *WorkersProvider) { p.Token = t } }

// WithEndpoint overrides the GraphQL endpoint (testing / gateways).
func WithEndpoint(u string) Option { return func(p *WorkersProvider) { p.Endpoint = u } }

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(p *WorkersProvider) { p.HTTP = c } }

// New builds a WorkersProvider from options.
func New(opts ...Option) *WorkersProvider {
	p := &WorkersProvider{Endpoint: defaultEndpoint}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Name identifies the provider.
func (p *WorkersProvider) Name() string { return "cloudflare-workers" }

// workersQuery aggregates request and error counts for one Worker over a window.
const workersQuery = `query ConsoleWorkerHealth($accountTag: string!, $scriptName: string!, $since: Time!) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      workersInvocationsAdaptive(limit: 100, filter: {scriptName: $scriptName, datetime_geq: $since}) {
        sum { requests errors }
      }
    }
  }
}`

// graphQLResponse models the slice of the analytics response we use.
type graphQLResponse struct {
	Data struct {
		Viewer struct {
			Accounts []struct {
				Invocations []struct {
					Sum struct {
						Requests int64 `json:"requests"`
						Errors   int64 `json:"errors"`
					} `json:"sum"`
				} `json:"workersInvocationsAdaptive"`
			} `json:"accounts"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Check queries the Worker's analytics and maps the error rate to a health state.
func (p *WorkersProvider) Check(ctx context.Context, comp core.Component) core.Check {
	check := core.Check{Component: comp.Key, CheckedAt: time.Now().UTC()}

	account := comp.Config["account_id"]
	script := comp.Config["worker"]
	if script == "" {
		script = comp.Config["script"]
	}
	token := comp.Config["api_token"]
	if token == "" {
		token = p.Token
	}
	if account == "" || script == "" || token == "" {
		check.State = core.StateUnknown
		check.Message = "missing account_id, worker, or api_token"
		return check
	}

	window := durationConfig(comp.Config["window"], defaultWindow)
	degradedPct := floatConfig(comp.Config["degraded_pct"], defaultDegradedPct)
	downPct := floatConfig(comp.Config["down_pct"], defaultDownPct)
	timeout := durationConfig(comp.Config["timeout"], defaultTimeout)

	since := time.Now().UTC().Add(-window).Format(time.RFC3339)
	body, _ := json.Marshal(map[string]any{
		"query": workersQuery,
		"variables": map[string]string{
			"accountTag": account,
			"scriptName": script,
			"since":      since,
		},
	})

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		check.State = core.StateUnknown
		check.Message = fmt.Sprintf("bad request: %v", err)
		return check
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	start := time.Now()
	resp, err := client.Do(req)
	check.Latency = time.Since(start)
	check.CheckedAt = time.Now().UTC()
	if err != nil {
		check.State = core.StateDown
		check.Message = fmt.Sprintf("cloudflare api request failed: %v", err)
		return check
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		check.State = core.StateDown
		check.Message = fmt.Sprintf("cloudflare api status %d: %s", resp.StatusCode, snippet(raw))
		return check
	}

	var gql graphQLResponse
	if err := json.Unmarshal(raw, &gql); err != nil {
		check.State = core.StateDown
		check.Message = fmt.Sprintf("decode cloudflare response: %v", err)
		return check
	}
	if len(gql.Errors) > 0 {
		check.State = core.StateDown
		check.Message = "cloudflare graphql error: " + gql.Errors[0].Message
		return check
	}

	var requests, errs int64
	for _, acct := range gql.Data.Viewer.Accounts {
		for _, inv := range acct.Invocations {
			requests += inv.Sum.Requests
			errs += inv.Sum.Errors
		}
	}

	if requests == 0 {
		check.State = core.StateUnknown
		check.Message = fmt.Sprintf("no invocations in last %s", window)
		return check
	}

	rate := float64(errs) / float64(requests) * 100
	switch {
	case rate >= downPct:
		check.State = core.StateDown
	case rate >= degradedPct:
		check.State = core.StateDegraded
	default:
		check.State = core.StateOperational
	}
	check.Message = fmt.Sprintf("errors %d/%d (%.2f%%) over %s", errs, requests, rate, window)
	return check
}

// durationConfig parses a Go duration from config, falling back to def.
func durationConfig(raw string, def time.Duration) time.Duration {
	if raw == "" {
		return def
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	return def
}

// floatConfig parses a float from config, falling back to def.
func floatConfig(raw string, def float64) float64 {
	if raw == "" {
		return def
	}
	var f float64
	if _, err := fmt.Sscanf(raw, "%f", &f); err == nil {
		return f
	}
	return def
}

// snippet trims an API error body for inclusion in a message.
func snippet(b []byte) string {
	const max = 160
	s := string(bytes.TrimSpace(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
