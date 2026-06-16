// Package openai provides an llm.Provider backed by OpenAI's Chat Completions
// API. It is a thin hand-written client over net/http; it pulls in no
// third-party deps and lives out of the core so the console binary does not link
// it (it ships as the console-plugin-openai executable).
package openai

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

	"github.com/moosequest/console/internal/llm"
)

// Default configuration for the OpenAI provider.
const (
	defaultOpenAIModel   = "gpt-4o-mini"
	defaultOpenAIBaseURL = "https://api.openai.com"
	defaultMaxTokens     = 1024
)

// OpenAI is an llm.Provider backed by OpenAI's Chat Completions API.
type OpenAI struct {
	APIKey  string
	Model   string
	BaseURL string
	HTTP    *http.Client
}

// Option configures an OpenAI provider in New.
type Option func(*OpenAI)

// WithModel sets the default model id used when a Request does not specify one.
func WithModel(model string) Option {
	return func(o *OpenAI) { o.Model = model }
}

// WithBaseURL overrides the API base URL (e.g. to point at a test server).
func WithBaseURL(baseURL string) Option {
	return func(o *OpenAI) { o.BaseURL = baseURL }
}

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(c *http.Client) Option {
	return func(o *OpenAI) { o.HTTP = c }
}

// New constructs an OpenAI provider. If apiKey is empty, it falls back to the
// OPENAI_API_KEY environment variable. Defaults: Model "gpt-4o-mini",
// BaseURL "https://api.openai.com", and a 60s HTTP client.
func New(apiKey string, opts ...Option) *OpenAI {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	o := &OpenAI{
		APIKey:  apiKey,
		Model:   defaultOpenAIModel,
		BaseURL: defaultOpenAIBaseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Name identifies the provider.
func (o *OpenAI) Name() string { return "openai" }

// openAIMessage is one turn in the Chat Completions API request body.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIRequest is the JSON body sent to POST /v1/chat/completions.
type openAIRequest struct {
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens"`
}

// openAIResponse is the subset of the Chat Completions API response we read.
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete sends a single completion request to the Chat Completions API and
// returns the text of the assistant's reply.
func (o *OpenAI) Complete(ctx context.Context, req llm.Request) (string, error) {
	if o.APIKey == "" {
		return "", fmt.Errorf("openai: no API key configured; set OPENAI_API_KEY")
	}

	model := o.Model
	if req.Model != "" {
		model = req.Model
	}
	maxTokens := defaultMaxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		messages = append(messages, openAIMessage{
			Role:    openAIRole(m.Role),
			Content: m.Text,
		})
	}

	body, err := json.Marshal(openAIRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("openai: encode request: %w", err)
	}

	url := strings.TrimRight(o.BaseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.HTTP.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("openai: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("openai: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai: unexpected status %d: %s", resp.StatusCode, snippet(respBody))
	}

	var parsed openAIResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("openai: decode response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return "", nil
	}
	return parsed.Choices[0].Message.Content, nil
}

// openAIRole maps an llm.Role to the API's role string.
func openAIRole(r llm.Role) string {
	switch r {
	case llm.RoleAssistant:
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

// Ensure OpenAI satisfies the Provider interface.
var _ llm.Provider = (*OpenAI)(nil)
