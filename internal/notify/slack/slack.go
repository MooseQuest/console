// Package slack delivers Console events to a Slack channel via an Incoming
// Webhook URL. Incoming Webhooks need no bot token or scopes — just the URL —
// which keeps setup trivial for self-hosters.
package slack

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

// Notifier posts events to a Slack Incoming Webhook.
type Notifier struct {
	// WebhookURL is the Slack Incoming Webhook endpoint.
	WebhookURL string
	// HTTP issues the POST. If nil, a client with a sane timeout is used.
	HTTP *http.Client
}

// Option configures a Notifier.
type Option func(*Notifier)

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(n *Notifier) { n.HTTP = c } }

// New builds a Slack notifier for the given Incoming Webhook URL.
func New(webhookURL string, opts ...Option) *Notifier {
	n := &Notifier{WebhookURL: webhookURL}
	for _, o := range opts {
		o(n)
	}
	return n
}

// Name identifies the sink.
func (n *Notifier) Name() string { return "slack" }

// colorFor maps an event severity to a Slack attachment color, reusing
// Console's palette.
func colorFor(severity string) string {
	switch severity {
	case "critical":
		return "#dc2626"
	case "warning":
		return "#d97706"
	case "good":
		return "#16a34a"
	default:
		return "#4f46e5"
	}
}

// Notify posts ev to the configured webhook as a colored attachment.
func (n *Notifier) Notify(ctx context.Context, ev core.Event) error {
	if n.WebhookURL == "" {
		return fmt.Errorf("slack: no webhook URL configured")
	}

	payload := map[string]any{
		"attachments": []map[string]any{{
			"color":  colorFor(ev.Type.Severity()),
			"title":  ev.Title,
			"text":   ev.Message,
			"footer": "Console",
			"ts":     ev.At.UTC().Unix(),
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := n.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("slack: status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
