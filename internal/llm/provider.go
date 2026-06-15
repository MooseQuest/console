// Package llm defines the provider seam for AI-Assisted onboarding. Provider is
// the plugin interface; Anthropic (Claude) is the default implementation and
// other providers can be registered behind the same interface.
package llm

import "context"

// Provider is a minimal text-completion interface. It is intentionally small so
// new providers (OpenAI, local models) are cheap to add.
type Provider interface {
	// Name identifies the provider, e.g. "anthropic".
	Name() string
	// Complete sends a system prompt plus a conversation and returns the
	// assistant's reply text.
	Complete(ctx context.Context, req Request) (string, error)
}

// Request is a single completion request.
type Request struct {
	System    string
	Messages  []Message
	MaxTokens int
	Model     string
}

// Role is the author of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn in a conversation.
type Message struct {
	Role Role
	Text string
}
