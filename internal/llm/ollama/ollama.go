// Package ollama provides an llm.Provider backed by a local Ollama server's
// chat API. It is a thin hand-written client over net/http; it pulls in no
// third-party deps and lives out of the core so the console binary does not link
// it (it ships as the console-plugin-ollama executable).
package ollama

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

// Default configuration for the Ollama provider.
const (
	defaultOllamaModel   = "llama3.1"
	defaultOllamaBaseURL = "http://localhost:11434"
)

// Ollama is an llm.Provider backed by a local Ollama server's chat API.
type Ollama struct {
	Model   string
	BaseURL string
	HTTP    *http.Client
}

// Option configures an Ollama provider in New.
type Option func(*Ollama)

// WithModel sets the default model id used when a Request does not specify one.
func WithModel(model string) Option {
	return func(o *Ollama) { o.Model = model }
}

// WithBaseURL overrides the server base URL (e.g. to point at a test server).
func WithBaseURL(baseURL string) Option {
	return func(o *Ollama) { o.BaseURL = baseURL }
}

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(c *http.Client) Option {
	return func(o *Ollama) { o.HTTP = c }
}

// New constructs an Ollama provider. No API key is required. Defaults: Model
// "llama3.1", BaseURL from OLLAMA_HOST else "http://localhost:11434", and a
// 120s HTTP client.
func New(opts ...Option) *Ollama {
	baseURL := defaultOllamaBaseURL
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		baseURL = host
	}
	o := &Ollama{
		Model:   defaultOllamaModel,
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Name identifies the provider.
func (o *Ollama) Name() string { return "ollama" }

// ollamaMessage is one turn in the chat API request body.
type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaRequest is the JSON body sent to POST /api/chat.
type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

// ollamaResponse is the subset of the chat API response we read.
type ollamaResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
}

// Complete sends a single completion request to the Ollama chat API and returns
// the text of the assistant's reply.
func (o *Ollama) Complete(ctx context.Context, req llm.Request) (string, error) {
	model := o.Model
	if req.Model != "" {
		model = req.Model
	}

	messages := make([]ollamaMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, ollamaMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		messages = append(messages, ollamaMessage{
			Role:    ollamaRole(m.Role),
			Content: m.Text,
		})
	}

	body, err := json.Marshal(ollamaRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	})
	if err != nil {
		return "", fmt.Errorf("ollama: encode request: %w", err)
	}

	url := strings.TrimRight(o.BaseURL, "/") + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.HTTP.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ollama: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ollama: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ollama: unexpected status %d: %s", resp.StatusCode, snippet(respBody))
	}

	var parsed ollamaResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("ollama: decode response: %w", err)
	}

	return parsed.Message.Content, nil
}

// ollamaRole maps an llm.Role to the API's role string.
func ollamaRole(r llm.Role) string {
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

// Ensure Ollama satisfies the Provider interface.
var _ llm.Provider = (*Ollama)(nil)
