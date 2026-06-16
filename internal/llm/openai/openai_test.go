package openai

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

func TestOpenAIComplete_Success(t *testing.T) {
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

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Hello, world."}}]}`)
	}))
	defer srv.Close()

	o := New("test-key", WithBaseURL(srv.URL), WithModel("gpt-4o-mini"))

	out, err := o.Complete(context.Background(), llm.Request{
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
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}

	if got := gotHeaders.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", got)
	}
	if got := gotHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	if gotBody["model"] != "gpt-4o-mini" {
		t.Errorf("body model = %v, want gpt-4o-mini", gotBody["model"])
	}
	if gotBody["max_tokens"] != float64(256) {
		t.Errorf("body max_tokens = %v, want 256", gotBody["max_tokens"])
	}

	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) != 4 {
		t.Fatalf("body messages = %v, want 4 entries (incl. system)", gotBody["messages"])
	}
	wantRoles := []string{"system", "user", "assistant", "user"}
	wantText := []string{"You are terse.", "Hi", "Hello", "Continue"}
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

func TestOpenAIComplete_DefaultModelAndMaxTokens(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	// No WithModel, no MaxTokens, no System -> provider defaults apply.
	o := New("k", WithBaseURL(srv.URL))
	if _, err := o.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "x"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotBody["model"] != defaultOpenAIModel {
		t.Errorf("model = %v, want %s", gotBody["model"], defaultOpenAIModel)
	}
	if gotBody["max_tokens"] != float64(defaultMaxTokens) {
		t.Errorf("max_tokens = %v, want %d", gotBody["max_tokens"], defaultMaxTokens)
	}
	// With empty System, only the single user message should be present.
	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages = %v, want 1 entry (no system)", gotBody["messages"])
	}
}

func TestOpenAIComplete_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()

	o := New("k", WithBaseURL(srv.URL))
	_, err := o.Complete(context.Background(), llm.Request{
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

func TestOpenAIComplete_EmptyAPIKeyNoNetwork(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "") // ensure the env fallback can't supply a key

	rt := &recordingTransport{}
	o := New("",
		WithBaseURL("http://127.0.0.1:0"), // would fail if dialed
		WithHTTPClient(&http.Client{Transport: rt}),
	)

	_, err := o.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "x"}},
	})
	if err == nil {
		t.Fatal("expected error for empty API key, got nil")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error = %q, want it to mention OPENAI_API_KEY", err.Error())
	}
	if rt.called {
		t.Error("HTTP transport was invoked; expected no network call when API key is empty")
	}
}

func TestNewOpenAIEnvFallback(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "from-env")
	o := New("")
	if o.APIKey != "from-env" {
		t.Errorf("APIKey = %q, want from-env", o.APIKey)
	}
	if o.Model != defaultOpenAIModel {
		t.Errorf("Model = %q, want %s", o.Model, defaultOpenAIModel)
	}
	if o.BaseURL != defaultOpenAIBaseURL {
		t.Errorf("BaseURL = %q, want %s", o.BaseURL, defaultOpenAIBaseURL)
	}
	if o.HTTP == nil {
		t.Error("HTTP client should be set by default")
	}
}
