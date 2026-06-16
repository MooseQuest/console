package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/llm"
)

func TestAnthropicComplete_Success(t *testing.T) {
	var (
		gotMethod  string
		gotPath    string
		gotHeaders http.Header
		gotBody    map[string]any
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()

		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("server: decode body: %v", err)
		}

		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"Hello, "},{"type":"text","text":"world."}]}`)
	}))
	defer srv.Close()

	a := New("test-key", WithBaseURL(srv.URL), WithModel("claude-opus-4-8"))

	out, err := a.Complete(context.Background(), llm.Request{
		System:    "You are terse.",
		MaxTokens: 256,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Text: "Hi"},
			{Role: llm.RoleAssistant, Text: "Hello"},
			{Role: llm.RoleUser, Text: "Continue"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if out != "Hello, world." {
		t.Errorf("output = %q, want %q", out, "Hello, world.")
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}

	if got := gotHeaders.Get("x-api-key"); got != "test-key" {
		t.Errorf("x-api-key = %q, want test-key", got)
	}
	if got := gotHeaders.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", got)
	}
	if got := gotHeaders.Get("content-type"); got != "application/json" {
		t.Errorf("content-type = %q, want application/json", got)
	}

	if gotBody["model"] != "claude-opus-4-8" {
		t.Errorf("body model = %v, want claude-opus-4-8", gotBody["model"])
	}
	if gotBody["max_tokens"] != float64(256) {
		t.Errorf("body max_tokens = %v, want 256", gotBody["max_tokens"])
	}
	if gotBody["system"] != "You are terse." {
		t.Errorf("body system = %v, want %q", gotBody["system"], "You are terse.")
	}

	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("body messages = %v, want 3 entries", gotBody["messages"])
	}
	wantRoles := []string{"user", "assistant", "user"}
	wantText := []string{"Hi", "Hello", "Continue"}
	for i, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] != wantRoles[i] {
			t.Errorf("message[%d] role = %v, want %s", i, mm["role"], wantRoles[i])
		}
		if mm["content"] != wantText[i] {
			t.Errorf("message[%d] content = %v, want %s", i, mm["content"], wantText[i])
		}
	}
}

func TestAnthropicComplete_DefaultModelAndMaxTokens(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer srv.Close()

	// No WithModel, no MaxTokens in the request -> provider defaults apply.
	a := New("k", WithBaseURL(srv.URL))
	if _, err := a.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "x"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotBody["model"] != defaultAnthropicModel {
		t.Errorf("model = %v, want %s", gotBody["model"], defaultAnthropicModel)
	}
	if gotBody["max_tokens"] != float64(defaultMaxTokens) {
		t.Errorf("max_tokens = %v, want %d", gotBody["max_tokens"], defaultMaxTokens)
	}
	// System is empty and must be omitted from the body.
	if _, present := gotBody["system"]; present {
		t.Errorf("system should be omitted when empty, got %v", gotBody["system"])
	}
}

func TestAnthropicComplete_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()

	a := New("k", WithBaseURL(srv.URL))
	_, err := a.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "x"}},
	})
	if err == nil {
		t.Fatal("expected error on non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, want it to include status 429", err.Error())
	}
	if !strings.Contains(err.Error(), "slow down") {
		t.Errorf("error = %q, want it to include a body snippet", err.Error())
	}
}

// recordingTransport fails the test if any HTTP round trip is attempted.
type recordingTransport struct{ called bool }

func (rt *recordingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	rt.called = true
	return nil, http.ErrUseLastResponse // never reached if logic is correct
}

func TestAnthropicComplete_EmptyAPIKeyNoNetwork(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "") // ensure the env fallback can't supply a key

	rt := &recordingTransport{}
	a := New("",
		WithBaseURL("http://127.0.0.1:0"), // would fail if dialed
		WithHTTPClient(&http.Client{Transport: rt}),
	)

	_, err := a.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "x"}},
	})
	if err == nil {
		t.Fatal("expected error for empty API key, got nil")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error = %q, want it to mention ANTHROPIC_API_KEY", err.Error())
	}
	if rt.called {
		t.Error("HTTP transport was invoked; expected no network call when API key is empty")
	}
}

func TestNewAnthropicEnvFallback(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	a := New("")
	if a.APIKey != "from-env" {
		t.Errorf("APIKey = %q, want from-env", a.APIKey)
	}
	if a.Model != defaultAnthropicModel {
		t.Errorf("Model = %q, want %s", a.Model, defaultAnthropicModel)
	}
	if a.BaseURL != defaultAnthropicBaseURL {
		t.Errorf("BaseURL = %q, want %s", a.BaseURL, defaultAnthropicBaseURL)
	}
	if a.HTTP == nil {
		t.Error("HTTP client should be set by default")
	}
}
