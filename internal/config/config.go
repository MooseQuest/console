// Package config holds Console's runtime configuration. Configuration is read
// from environment variables with sensible defaults; the CLI layers flags on
// top. Keeping this dependency-free (env + defaults, no YAML parser) keeps the
// binary small and the config surface obvious.
package config

import (
	"os"
	"strings"
)

// Config is the resolved runtime configuration.
type Config struct {
	// Addr is the HTTP listen address, e.g. ":8080".
	Addr string
	// DB is the storage DSN. For SQLite this is a file path ("console.db") or
	// "" / ":memory:" for an in-memory database.
	DB string
	// LLMProvider selects the AI-Assisted onboarding backend, e.g. "anthropic".
	// Empty disables AI-Assisted mode (Human mode still works).
	LLMProvider string
	// AnthropicKey is the API key for the Anthropic provider. Falls back to the
	// ANTHROPIC_API_KEY environment variable when constructing the provider.
	AnthropicKey string
	// Model overrides the default LLM model when set.
	Model string
}

// Default returns the baseline configuration before env/flag overrides.
func Default() Config {
	return Config{
		Addr:        ":8080",
		DB:          "console.db",
		LLMProvider: "anthropic",
	}
}

// FromEnv returns the default config with any CONSOLE_* environment overrides
// applied. Recognized variables:
//
//	CONSOLE_ADDR          HTTP listen address (default ":8080")
//	CONSOLE_DB            storage DSN / SQLite path (default "console.db")
//	CONSOLE_LLM_PROVIDER  LLM provider name (default "anthropic", "" to disable)
//	CONSOLE_MODEL         LLM model override
//	ANTHROPIC_API_KEY     Anthropic API key
func FromEnv() Config {
	c := Default()
	if v := os.Getenv("CONSOLE_ADDR"); v != "" {
		c.Addr = v
	}
	if v, ok := os.LookupEnv("CONSOLE_DB"); ok {
		c.DB = v
	}
	if v, ok := os.LookupEnv("CONSOLE_LLM_PROVIDER"); ok {
		c.LLMProvider = strings.TrimSpace(v)
	}
	if v := os.Getenv("CONSOLE_MODEL"); v != "" {
		c.Model = v
	}
	c.AnthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	return c
}
