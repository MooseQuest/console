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
	// Addr is the HTTP listen address. Defaults to loopback (127.0.0.1:8080)
	// because the API and dashboard have no built-in authentication yet —
	// exposing them requires an authenticating reverse proxy. Set ":8080" (all
	// interfaces) deliberately, behind such a proxy. See docs/security/.
	Addr string
	// DB is the storage DSN. For SQLite this is a file path ("console.db") or
	// "" / ":memory:" for an in-memory database.
	DB string
	// StorePlugin is the path to an out-of-process storage-backend plugin
	// executable (e.g. console-plugin-postgres). When set, it replaces the
	// built-in SQLite store; the plugin inherits this process's environment.
	StorePlugin string
	// NotifyPlugins are paths to out-of-process notifier plugin executables
	// (e.g. console-plugin-slack). Each is launched and registered as a sink;
	// plugins inherit this process's environment (e.g. CONSOLE_SLACK_WEBHOOK_URL).
	NotifyPlugins []string
	// StatusPlugins are paths to out-of-process status-provider plugin
	// executables (e.g. console-plugin-cloudflare). Each is launched and
	// registered with the status engine; plugins inherit this process's
	// environment (e.g. CLOUDFLARE_API_TOKEN).
	StatusPlugins []string
	// LLMPlugin is the path to an out-of-process LLM provider plugin executable
	// (e.g. console-plugin-anthropic). When set, it supplies the AI-Assisted
	// onboarding provider over gRPC; empty disables AI-Assisted mode. The plugin
	// inherits this process's environment (e.g. ANTHROPIC_API_KEY).
	LLMPlugin string
}

// Default returns the baseline configuration before env/flag overrides.
func Default() Config {
	return Config{
		Addr: "127.0.0.1:8080",
		DB:   "console.db",
	}
}

// FromEnv returns the default config with any CONSOLE_* environment overrides
// applied. Recognized variables:
//
//	CONSOLE_ADDR          HTTP listen address (default "127.0.0.1:8080", loopback)
//	CONSOLE_DB            storage DSN / SQLite path (default "console.db")
//	CONSOLE_STORE_PLUGIN   path to an out-of-process storage-backend plugin
//	CONSOLE_NOTIFY_PLUGINS comma/space-separated notifier plugin paths
//	CONSOLE_STATUS_PLUGINS comma/space-separated status-provider plugin paths
//	CONSOLE_LLM_PLUGIN     path to an out-of-process LLM provider plugin
//
// Provider-specific variables (e.g. ANTHROPIC_API_KEY, CLOUDFLARE_API_TOKEN,
// CONSOLE_SLACK_WEBHOOK_URL, CONSOLE_MODEL) are read by the relevant plugin,
// which inherits this process's environment — the host does not consume them.
func FromEnv() Config {
	c := Default()
	if v := os.Getenv("CONSOLE_ADDR"); v != "" {
		c.Addr = v
	}
	if v, ok := os.LookupEnv("CONSOLE_DB"); ok {
		c.DB = v
	}
	c.StorePlugin = os.Getenv("CONSOLE_STORE_PLUGIN")
	c.NotifyPlugins = splitList(os.Getenv("CONSOLE_NOTIFY_PLUGINS"))
	c.StatusPlugins = splitList(os.Getenv("CONSOLE_STATUS_PLUGINS"))
	c.LLMPlugin = os.Getenv("CONSOLE_LLM_PLUGIN")
	return c
}

// splitList splits a comma- or space-separated list, trimming blanks.
func splitList(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
