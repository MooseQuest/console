package plugin

import (
	"context"
	"sync"
	"testing"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/moosequest/console/internal/llm"
)

// recordingLLM captures the request it received and returns a fixed reply
// (server side of the test).
type recordingLLM struct {
	mu  sync.Mutex
	got llm.Request
	out string
}

func (l *recordingLLM) Name() string { return "recorder" }

func (l *recordingLLM) Complete(_ context.Context, req llm.Request) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = req
	return l.out, nil
}

func TestLLMPlugin_RoundTrip(t *testing.T) {
	backing := &recordingLLM{out: "Hello, world."}
	client, server := goplugin.TestPluginGRPCConn(t, false, map[string]goplugin.Plugin{
		LLMPluginName: &LLMPlugin{Impl: backing},
	})
	t.Cleanup(func() { _ = client.Close(); server.Stop() })

	raw, err := client.Dispense(LLMPluginName)
	if err != nil {
		t.Fatalf("dispense: %v", err)
	}
	p, ok := raw.(llm.Provider)
	if !ok {
		t.Fatalf("dispensed %T, want llm.Provider", raw)
	}

	if p.Name() != "recorder" {
		t.Errorf("Name() over gRPC = %q, want recorder", p.Name())
	}

	req := llm.Request{
		System:    "You are terse.",
		MaxTokens: 256,
		Model:     "claude-opus-4-8",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Text: "Hi"},
			{Role: llm.RoleAssistant, Text: "Hello"},
			{Role: llm.RoleUser, Text: "Continue"},
		},
	}
	out, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "Hello, world." {
		t.Errorf("output = %q, want %q", out, "Hello, world.")
	}

	backing.mu.Lock()
	defer backing.mu.Unlock()
	if backing.got.System != "You are terse." || backing.got.MaxTokens != 256 ||
		backing.got.Model != "claude-opus-4-8" || len(backing.got.Messages) != 3 {
		t.Fatalf("request did not round-trip: %+v", backing.got)
	}
	if backing.got.Messages[1].Role != llm.RoleAssistant || backing.got.Messages[1].Text != "Hello" {
		t.Errorf("message[1] = %+v, want assistant/Hello", backing.got.Messages[1])
	}
}
