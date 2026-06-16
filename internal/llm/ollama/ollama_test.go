package ollama

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

func TestOllamaComplete_Success(t *testing.T) {
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
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"Hello, world."}}`)
	}))
	defer srv.Close()

	o := New(WithBaseURL(srv.URL), WithModel("llama3.1"))

	out, err := o.Complete(context.Background(), llm.Request{
		System: "You are terse.",
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
	if gotPath != "/api/chat" {
		t.Errorf("path = %q, want /api/chat", gotPath)
	}
	if got := gotHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	if gotBody["model"] != "llama3.1" {
		t.Errorf("body model = %v, want llama3.1", gotBody["model"])
	}
	if gotBody["stream"] != false {
		t.Errorf("body stream = %v, want false", gotBody["stream"])
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

func TestOllamaComplete_DefaultModel(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"ok"}}`)
	}))
	defer srv.Close()

	o := New(WithBaseURL(srv.URL))
	if _, err := o.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "x"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotBody["model"] != defaultOllamaModel {
		t.Errorf("model = %v, want %s", gotBody["model"], defaultOllamaModel)
	}
	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages = %v, want 1 entry (no system)", gotBody["messages"])
	}
}

func TestOllamaComplete_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"model not found"}`)
	}))
	defer srv.Close()

	o := New(WithBaseURL(srv.URL))
	_, err := o.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "x"}},
	})
	if err == nil {
		t.Fatal("expected error on non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to include status 500", err.Error())
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error = %q, want it to include a body snippet", err.Error())
	}
}

func TestNewOllamaDefaults(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "")
	o := New()
	if o.Model != defaultOllamaModel {
		t.Errorf("Model = %q, want %s", o.Model, defaultOllamaModel)
	}
	if o.BaseURL != defaultOllamaBaseURL {
		t.Errorf("BaseURL = %q, want %s", o.BaseURL, defaultOllamaBaseURL)
	}
	if o.HTTP == nil {
		t.Error("HTTP client should be set by default")
	}
}

func TestNewOllamaHostEnv(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://example.com:1234")
	o := New()
	if o.BaseURL != "http://example.com:1234" {
		t.Errorf("BaseURL = %q, want http://example.com:1234", o.BaseURL)
	}
}
