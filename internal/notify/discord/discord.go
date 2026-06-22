// Package discord delivers Console events to a Discord channel via a channel
// Webhook URL. Discord webhooks need no bot token or scopes — just the URL —
// which keeps setup trivial for self-hosters, the same way the Slack sink does.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/moosequest/console/internal/core"
)

// Notifier posts events to a Discord channel Webhook.
type Notifier struct {
	// WebhookURL is the Discord channel Webhook endpoint.
	WebhookURL string
	// HTTP issues the POST. If nil, a client with a sane timeout is used.
	HTTP *http.Client
}

// Option configures a Notifier.
type Option func(*Notifier)

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(n *Notifier) { n.HTTP = c } }

// New builds a Discord notifier for the given channel Webhook URL.
func New(webhookURL string, opts ...Option) *Notifier {
	n := &Notifier{WebhookURL: webhookURL}
	for _, o := range opts {
		o(n)
	}
	return n
}

// Name identifies the sink.
func (n *Notifier) Name() string { return "discord" }

// colorFor maps an event severity to a Discord embed color, reusing Console's
// palette. Discord expects a decimal-encoded RGB integer rather than a hex
// string, but the underlying colors match the Slack sink.
func colorFor(severity string) int {
	switch severity {
	case "critical":
		return 0xDC2626
	case "warning":
		return 0xD97706
	case "good":
		return 0x16A34A
	default:
		return 0x4F46E5
	}
}

// Notify posts ev to the configured webhook as a single colored embed.
func (n *Notifier) Notify(ctx context.Context, ev core.Event) error {
	if n.WebhookURL == "" {
		return fmt.Errorf("discord: no webhook URL configured")
	}

	embed := map[string]any{
		"title":     ev.Title,
		"color":     colorFor(ev.Type.Severity()),
		"timestamp": ev.At.UTC().Format(time.RFC3339),
		"footer":    map[string]any{"text": "Console"},
	}
	if ev.Message != "" {
		embed["description"] = ev.Message
	}
	payload := map[string]any{"embeds": []map[string]any{embed}}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("discord: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := n.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		// A Discord webhook URL is itself a secret; strip it from the error so
		// it never reaches logs.
		return fmt.Errorf("discord: post failed: %v", redactURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("discord: status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}

// redactURL strips any embedded request URL from err (a Discord webhook URL is
// a credential), returning the underlying cause so logs never expose it.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
