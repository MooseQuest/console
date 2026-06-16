// Package sentry provides Console status providers backed by Sentry's API.
// Provider reports the health of a Sentry project from its current unresolved
// issue count (GET /api/0/projects/{org}/{project}/issues/?query=is:unresolved).
// The count is accurate up to one page (100) and saturates beyond that.
//
// It satisfies the status.Provider interface structurally (Name + Check), so it
// does not import the status package and adds no coupling beyond core.
package sentry

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

// defaultBaseURL is the Sentry API base URL.
const defaultBaseURL = "https://sentry.io"

// Default thresholds and timeout used when a component does not override them.
const (
	defaultDegradedCount = 1
	defaultDownCount     = 10
	defaultTimeout       = 10 * time.Second
)

// Provider checks a Sentry project's health from its unresolved issue count.
// The number of currently unresolved issues maps to a health state.
//
// It reads its target from core.Component.Config:
//
//	org             required; the Sentry organization slug
//	project         required; the Sentry project slug
//	auth_token      optional; overrides the provider's default token
//	degraded_count  optional; issue count at/above which state is degraded (default 1)
//	down_count      optional; issue count at/above which state is down (default 10)
//	timeout         optional; API call timeout as a Go duration, default "10s"
//
// State mapping (count = number of currently unresolved issues, capped at 100):
//
//	count < degraded_count                  -> StateOperational
//	degraded_count <= count < down_count    -> StateDegraded
//	count >= down_count                     -> StateDown
//	API/network failure or non-2xx          -> StateDown
//	no org / project / token                -> StateUnknown (configuration gap)
type Provider struct {
	// Token is the default Sentry auth token, used when a component does not set
	// "auth_token". Typically sourced from the SENTRY_AUTH_TOKEN env var.
	Token string
	// BaseURL is the Sentry API base URL; defaults to Sentry's. Overridable for
	// testing or a self-hosted instance.
	BaseURL string
	// HTTP issues the API request. If nil, a client with a sane timeout is used.
	HTTP *http.Client
}

// Option configures a Provider.
type Option func(*Provider)

// WithToken sets the default auth token.
func WithToken(t string) Option { return func(p *Provider) { p.Token = t } }

// WithBaseURL overrides the Sentry API base URL (testing / self-hosted).
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
func (p *Provider) Name() string { return "sentry" }

// issue models the slice of a Sentry issue object we use. Only the presence of
// an element matters, so no fields are decoded.
type issue struct{}

// Check fetches the project's unresolved issues and maps their count to a health state.
func (p *Provider) Check(ctx context.Context, comp core.Component) core.Check {
	check := core.Check{Component: comp.Key, CheckedAt: time.Now().UTC()}

	org := comp.Config["org"]
	project := comp.Config["project"]
	token := comp.Config["auth_token"]
	if token == "" {
		token = p.Token
	}
	if org == "" || project == "" || token == "" {
		check.State = core.StateUnknown
		check.Message = "missing org, project, or auth_token"
		return check
	}

	degradedCount := intConfig(comp.Config["degraded_count"], defaultDegradedCount)
	downCount := intConfig(comp.Config["down_count"], defaultDownCount)
	timeout := durationConfig(comp.Config["timeout"], defaultTimeout)

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	// limit=100 is the max page size; the unresolved count is therefore accurate
	// up to 100 (it saturates beyond that — reported as "100+"). The issues
	// endpoint returns one page of unresolved issues; we count them.
	url := fmt.Sprintf("%s/api/0/projects/%s/%s/issues/?query=is:unresolved&limit=%d", baseURL, org, project, issuePageLimit)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		check.State = core.StateUnknown
		check.Message = fmt.Sprintf("bad request: %v", err)
		return check
	}
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
		check.Message = fmt.Sprintf("sentry api request failed: %v", err)
		return check
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		check.State = core.StateDown
		check.Message = fmt.Sprintf("sentry api status %d: %s", resp.StatusCode, snippet(raw))
		return check
	}

	var issues []issue
	if err := json.Unmarshal(raw, &issues); err != nil {
		check.State = core.StateDown
		check.Message = fmt.Sprintf("decode sentry response: %v", err)
		return check
	}

	count := len(issues)
	switch {
	case count >= downCount:
		check.State = core.StateDown
	case count >= degradedCount:
		check.State = core.StateDegraded
	default:
		check.State = core.StateOperational
	}
	if count >= issuePageLimit {
		check.Message = fmt.Sprintf("%d+ unresolved issues", issuePageLimit)
	} else {
		check.Message = fmt.Sprintf("%d unresolved issues", count)
	}
	return check
}

// issuePageLimit is the page size requested from Sentry; the unresolved count is
// accurate up to this value and saturates beyond it.
const issuePageLimit = 100

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

// intConfig parses an int from config, falling back to def.
func intConfig(raw string, def int) int {
	if raw == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err == nil {
		return n
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
