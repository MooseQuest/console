package status

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/moosequest/console/internal/core"
)

// defaultHTTPTimeout bounds a single probe when comp.Config has no "timeout".
const defaultHTTPTimeout = 5 * time.Second

// HTTPProvider checks a component by issuing an HTTP request and inspecting the
// response status.
//
// It reads its target from core.Component.Config:
//
//	url           required; the endpoint to probe
//	method        optional; request method, default "GET"
//	expect_status optional; an exact status code that means operational
//	timeout       optional; a Go duration string (e.g. "2s"), default 5s
//
// State mapping:
//
//	2xx, or the exact expect_status if set -> StateOperational
//	any other HTTP response received       -> StateDegraded
//	connection error or timeout            -> StateDown
//	missing "url"                          -> StateUnknown
type HTTPProvider struct {
	// Client issues the probe request. If nil, http.DefaultClient is used. A
	// per-request timeout is applied via context regardless of Client.Timeout.
	Client *http.Client
}

// Name identifies the provider.
func (p *HTTPProvider) Name() string { return "http" }

// Check probes comp's "url" and maps the result to a core.Check.
func (p *HTTPProvider) Check(ctx context.Context, comp core.Component) core.Check {
	check := core.Check{
		Component: comp.Key,
		CheckedAt: time.Now().UTC(),
	}

	url := comp.Config["url"]
	if url == "" {
		check.State = core.StateUnknown
		check.Message = "no url configured"
		return check
	}

	method := comp.Config["method"]
	if method == "" {
		method = http.MethodGet
	}

	timeout := defaultHTTPTimeout
	if raw := comp.Config["timeout"]; raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			check.State = core.StateUnknown
			check.Message = fmt.Sprintf("invalid timeout %q: %v", raw, err)
			check.CheckedAt = time.Now().UTC()
			return check
		}
		timeout = d
	}

	var expectStatus int
	if raw := comp.Config["expect_status"]; raw != "" {
		code, err := strconv.Atoi(raw)
		if err != nil {
			check.State = core.StateUnknown
			check.Message = fmt.Sprintf("invalid expect_status %q: %v", raw, err)
			check.CheckedAt = time.Now().UTC()
			return check
		}
		expectStatus = code
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, url, nil)
	if err != nil {
		check.State = core.StateUnknown
		check.Message = fmt.Sprintf("bad request: %v", err)
		check.CheckedAt = time.Now().UTC()
		return check
	}

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}

	start := time.Now()
	resp, err := client.Do(req)
	check.Latency = time.Since(start)
	check.CheckedAt = time.Now().UTC()
	if err != nil {
		check.State = core.StateDown
		check.Message = fmt.Sprintf("request failed: %v", err)
		return check
	}
	defer resp.Body.Close()

	switch {
	case expectStatus != 0:
		if resp.StatusCode == expectStatus {
			check.State = core.StateOperational
			check.Message = fmt.Sprintf("status %d", resp.StatusCode)
		} else {
			check.State = core.StateDegraded
			check.Message = fmt.Sprintf("status %d, expected %d", resp.StatusCode, expectStatus)
		}
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		check.State = core.StateOperational
		check.Message = fmt.Sprintf("status %d", resp.StatusCode)
	default:
		check.State = core.StateDegraded
		check.Message = fmt.Sprintf("status %d", resp.StatusCode)
	}

	return check
}
