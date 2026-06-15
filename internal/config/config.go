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
	// CloudflareToken is the default Cloudflare API token used by Cloudflare
	// status providers when a component does not set its own "api_token".
	CloudflareToken string
	// SlackWebhookURL, when set, enables Slack notifications via an Incoming
	// Webhook for status transitions and flag changes.
	SlackWebhookURL string
	// StorePlugin is the path to an out-of-process storage-backend plugin
	// executable (e.g. console-plugin-postgres). When set, it replaces the
	// built-in SQLite store; the plugin inherits this process's environment.
	StorePlugin string
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
//	CLOUDFLARE_API_TOKEN  default token for Cloudflare status providers
//	CONSOLE_SLACK_WEBHOOK_URL  Slack Incoming Webhook for notifications
//	CONSOLE_STORE_PLUGIN  path to an out-of-process storage-backend plugin
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
	c.CloudflareToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	c.SlackWebhookURL = os.Getenv("CONSOLE_SLACK_WEBHOOK_URL")
	c.StorePlugin = os.Getenv("CONSOLE_STORE_PLUGIN")
	return c
}
