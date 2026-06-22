// Package pagerduty delivers Console events to PagerDuty via the Events API v2
// (https://events.pagerduty.com/v2/enqueue). A component going down or degraded
// triggers an alert; the matching recovery resolves it, correlated by a
// deterministic dedup key so PagerDuty groups the open/close pair into a single
// incident.
//
// PagerDuty is a paging sink, so only component health transitions are
// delivered — flag-change events are not incidents and are skipped.
package pagerduty

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

// defaultEndpoint is the PagerDuty Events API v2 enqueue URL.
const defaultEndpoint = "https://events.pagerduty.com/v2/enqueue"

// Notifier posts events to PagerDuty's Events API v2.
type Notifier struct {
	// RoutingKey is the integration key of the target PagerDuty service.
	RoutingKey string
	// Endpoint is the enqueue URL; defaults to the public Events API v2 endpoint.
	Endpoint string
	// HTTP issues the POST. If nil, a client with a sane timeout is used.
	HTTP *http.Client
}

// Option configures a Notifier.
type Option func(*Notifier)

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(n *Notifier) { n.HTTP = c } }

// WithEndpoint overrides the enqueue URL (used in tests).
func WithEndpoint(url string) Option { return func(n *Notifier) { n.Endpoint = url } }

// New builds a PagerDuty notifier for the given routing (integration) key.
func New(routingKey string, opts ...Option) *Notifier {
	n := &Notifier{RoutingKey: routingKey, Endpoint: defaultEndpoint}
	for _, o := range opts {
		o(n)
	}
	return n
}

// Name identifies the sink.
func (n *Notifier) Name() string { return "pagerduty" }

// severityFor maps an event to a PagerDuty alert severity (one of critical,
// error, warning, info).
func severityFor(et core.EventType) string {
	switch et {
	case core.EventComponentDown:
		return "critical"
	case core.EventComponentDegraded:
		return "warning"
	default:
		return "info"
	}
}

// dedupKey correlates a trigger with its later resolve for the same component,
// so PagerDuty closes the incident it opened rather than opening a second one.
func dedupKey(ev core.Event) string { return "console/" + ev.Component }

// Notify triggers or resolves a PagerDuty alert for ev. Down/degraded
// transitions trigger; a recovery resolves the correlated alert. Events that
// are not component health transitions (e.g. flag changes) are skipped.
func (n *Notifier) Notify(ctx context.Context, ev core.Event) error {
	if n.RoutingKey == "" {
		return fmt.Errorf("pagerduty: no routing key configured")
	}

	var action string
	switch ev.Type {
	case core.EventComponentDown, core.EventComponentDegraded:
		action = "trigger"
	case core.EventComponentRecovered:
		action = "resolve"
	default:
		// Not a pageable incident — nothing to do.
		return nil
	}

	payload := map[string]any{
		"routing_key":  n.RoutingKey,
		"event_action": action,
		"dedup_key":    dedupKey(ev),
	}
	if action == "trigger" {
		details := map[string]any{
			"summary":   ev.Title,
			"source":    "console",
			"severity":  severityFor(ev.Type),
			"timestamp": ev.At.UTC().Format(time.RFC3339),
		}
		if ev.Component != "" {
			details["component"] = ev.Component
		}
		if ev.Message != "" {
			details["custom_details"] = map[string]any{"message": ev.Message}
		}
		payload["payload"] = details
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pagerduty: marshal: %w", err)
	}

	endpoint := n.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pagerduty: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := n.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty: post failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("pagerduty: status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
