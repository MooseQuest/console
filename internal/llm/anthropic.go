package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Default configuration for the Anthropic provider.
const (
	defaultAnthropicModel   = "claude-opus-4-8"
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	defaultMaxTokens        = 1024
	anthropicVersion        = "2023-06-01"
)

// Anthropic is a Provider backed by Anthropic's Claude Messages API. It is a
// thin hand-written client over net/http; it pulls in no third-party deps.
type Anthropic struct {
	APIKey  string
	Model   string
	BaseURL string
	HTTP    *http.Client
}

// Option configures an Anthropic provider in NewAnthropic.
type Option func(*Anthropic)

// WithModel sets the default model id used when a Request does not specify one.
func WithModel(model string) Option {
	return func(a *Anthropic) { a.Model = model }
}

// WithBaseURL overrides the API base URL (e.g. to point at a test server).
func WithBaseURL(baseURL string) Option {
	return func(a *Anthropic) { a.BaseURL = baseURL }
}

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(c *http.Client) Option {
	return func(a *Anthropic) { a.HTTP = c }
}

// NewAnthropic constructs an Anthropic provider. If apiKey is empty, it falls
// back to the ANTHROPIC_API_KEY environment variable. Defaults: Model
// "claude-opus-4-8", BaseURL "https://api.anthropic.com", and a 60s HTTP client.
func NewAnthropic(apiKey string, opts ...Option) *Anthropic {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	a := &Anthropic{
		APIKey:  apiKey,
		Model:   defaultAnthropicModel,
		BaseURL: defaultAnthropicBaseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Name identifies the provider.
func (a *Anthropic) Name() string { return "anthropic" }

// anthropicMessage is one turn in the Messages API request body.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicRequest is the JSON body sent to POST /v1/messages.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicResponse is the subset of the Messages API response we read.
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Complete sends a single completion request to the Messages API and returns
// the concatenated text of the assistant's reply.
func (a *Anthropic) Complete(ctx context.Context, req Request) (string, error) {
	if a.APIKey == "" {
		return "", fmt.Errorf("anthropic: no API key configured; set ANTHROPIC_API_KEY")
	}

	model := a.Model
	if req.Model != "" {
		model = req.Model
	}
	maxTokens := defaultMaxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	messages := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, anthropicMessage{
			Role:    anthropicRole(m.Role),
			Content: m.Text,
		})
	}

	body, err := json.Marshal(anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    req.System,
		Messages:  messages,
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: encode request: %w", err)
	}

	url := strings.TrimRight(a.BaseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := a.HTTP.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("anthropic: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("anthropic: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("anthropic: unexpected status %d: %s", resp.StatusCode, snippet(respBody))
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("anthropic: decode response: %w", err)
	}

	var sb strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String(), nil
}

// anthropicRole maps an llm.Role to the API's role string.
func anthropicRole(r Role) string {
	switch r {
	case RoleAssistant:
		return "assistant"
	default:
		return "user"
	}
}

// snippet returns a bounded, single-line view of a response body for errors.
func snippet(b []byte) string {
	const max = 512
	s := string(b)
	if len(s) > max {
		s = s[:max] + "..."
	}
	return strings.TrimSpace(s)
}

// Ensure Anthropic satisfies the Provider interface.
var _ Provider = (*Anthropic)(nil)
