// Package heroku provides Console status providers backed by Heroku's Platform
// API. Provider reports the health of a Heroku app from the state of its dynos
// (GET /apps/{app}/dynos).
//
// It satisfies the status.Provider interface structurally (Name + Check), so it
// does not import the status package and adds no coupling beyond core.
package heroku

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

// defaultBaseURL is the Heroku Platform API base URL.
const defaultBaseURL = "https://api.heroku.com"

// defaultTimeout is the API call timeout used when a component does not override it.
const defaultTimeout = 10 * time.Second

// Provider checks a Heroku app's health from the state of its dynos. The
// fraction of dynos reporting "up" maps to a health state.
//
// It reads its target from core.Component.Config:
//
//	app        required; the Heroku app name
//	api_token  optional; overrides the provider's default token
//	timeout    optional; API call timeout as a Go duration, default "10s"
//
// State mapping (over the dynos returned by the API):
//
//	all dynos up (and >=1 dyno)         -> StateOperational
//	at least one up but others not      -> StateDegraded
//	zero dynos up                       -> StateDown
//	API/network failure or non-2xx      -> StateDown
//	no app / token                      -> StateUnknown (configuration gap)
//	zero dynos returned                 -> StateUnknown (no dynos)
type Provider struct {
	// Token is the default Heroku API token, used when a component does not set
	// "api_token". Typically sourced from the HEROKU_API_KEY env var.
	Token string
	// BaseURL is the Platform API base URL; defaults to Heroku's. Overridable for
	// testing or a self-hosted gateway.
	BaseURL string
	// HTTP issues the API request. If nil, a client with a sane timeout is used.
	HTTP *http.Client
}

// Option configures a Provider.
type Option func(*Provider)

// WithToken sets the default API token.
func WithToken(t string) Option { return func(p *Provider) { p.Token = t } }

// WithBaseURL overrides the Platform API base URL (testing / gateways).
func WithBaseURL(u string) Option { return func(p *Provider) { p.BaseURL = u } }

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(p *Provider) { p.HTTP = c } }

// New builds a Provider from options.
func New(opts ...Option) *Provider {
	p := &Provider{BaseURL: defaultBaseURL}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Name identifies the provider.
func (p *Provider) Name() string { return "heroku" }

// dyno models the slice of a Heroku dyno object we use.
type dyno struct {
	State string `json:"state"`
}

// Check fetches the app's dynos and maps their states to a health state.
func (p *Provider) Check(ctx context.Context, comp core.Component) core.Check {
	check := core.Check{Component: comp.Key, CheckedAt: time.Now().UTC()}

	app := comp.Config["app"]
	token := comp.Config["api_token"]
	if token == "" {
		token = p.Token
	}
	if app == "" || token == "" {
		check.State = core.StateUnknown
		check.Message = "missing app or api_token"
		return check
	}

	timeout := durationConfig(comp.Config["timeout"], defaultTimeout)

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	url := fmt.Sprintf("%s/apps/%s/dynos", baseURL, app)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		check.State = core.StateUnknown
		check.Message = fmt.Sprintf("bad request: %v", err)
		return check
	}
	req.Header.Set("Accept", "application/vnd.heroku+json; version=3")
	req.Header.Set("Authorization", "Bearer "+token)

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
		check.Message = fmt.Sprintf("heroku api request failed: %v", err)
		return check
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		check.State = core.StateDown
		check.Message = fmt.Sprintf("heroku api status %d: %s", resp.StatusCode, snippet(raw))
		return check
	}

	var dynos []dyno
	if err := json.Unmarshal(raw, &dynos); err != nil {
		check.State = core.StateDown
		check.Message = fmt.Sprintf("decode heroku response: %v", err)
		return check
	}

	if len(dynos) == 0 {
		check.State = core.StateUnknown
		check.Message = "no dynos"
		return check
	}

	up := 0
	for _, d := range dynos {
		if d.State == "up" {
			up++
		}
	}
	total := len(dynos)

	switch {
	case up == 0:
		check.State = core.StateDown
	case up == total:
		check.State = core.StateOperational
	default:
		check.State = core.StateDegraded
	}
	check.Message = fmt.Sprintf("%d/%d dynos up", up, total)
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

// snippet trims an API error body for inclusion in a message.
func snippet(b []byte) string {
	const max = 160
	s := string(bytes.TrimSpace(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
