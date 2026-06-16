// Package webhook delivers Console events to an arbitrary HTTP endpoint as a
// JSON POST. It is the generic sink: any service that can accept a JSON webhook
// (a custom handler, an automation platform, a serverless function) can consume
// Console events without a bespoke integration.
//
// When a Secret is configured it is sent verbatim in the X-Webhook-Secret
// request header, letting the receiver authenticate the caller with a simple
// shared-secret check.
package webhook

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

// Notifier posts events to a configured HTTP endpoint as JSON.
type Notifier struct {
	// URL is the endpoint the event is POSTed to.
	URL string
	// Secret, when set, is sent in the X-Webhook-Secret header so the receiver
	// can authenticate the caller.
	Secret string
	// HTTP issues the POST. If nil, a client with a sane timeout is used.
	HTTP *http.Client
}

// Option configures a Notifier.
type Option func(*Notifier)

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(n *Notifier) { n.HTTP = c } }

// WithSecret sets the shared secret sent in the X-Webhook-Secret header.
func WithSecret(s string) Option { return func(n *Notifier) { n.Secret = s } }

// New builds a webhook notifier for the given endpoint URL.
func New(url string, opts ...Option) *Notifier {
	n := &Notifier{URL: url}
	for _, o := range opts {
		o(n)
	}
	return n
}

// Name identifies the sink.
func (n *Notifier) Name() string { return "webhook" }

// Notify POSTs ev to the configured URL as a JSON object. The event's own JSON
// fields are emitted, plus a top-level "severity" derived from the event type.
func (n *Notifier) Notify(ctx context.Context, ev core.Event) error {
	if n.URL == "" {
		return fmt.Errorf("webhook: no URL configured")
	}

	// Marshal the event, then splice in a top-level "severity" so receivers can
	// route without re-deriving it from the type.
	raw, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}
	fields["severity"] = ev.Type.Severity()
	body, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if n.Secret != "" {
		req.Header.Set("X-Webhook-Secret", n.Secret)
	}

	client := n.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("webhook: status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
