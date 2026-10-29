package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
)

// Auth modes accepted by Config.AuthMode (CONSOLE_AUTH).
const (
	// AuthAuto enables authentication whenever at least one token is configured.
	AuthAuto = "auto"
	// AuthDisabled turns authentication off even if tokens are configured. It is
	// the documented escape hatch for deployments fronted by an authenticating
	// proxy; it is never used in quickstarts.
	AuthDisabled = "disabled"
)

// Session cookie Secure-attribute modes accepted by Config.CookieSecure
// (CONSOLE_COOKIE_SECURE).
const (
	// CookieSecureAuto sets Secure when the request is TLS or carries
	// X-Forwarded-Proto: https.
	CookieSecureAuto = "auto"
	// CookieSecureAlways always sets Secure.
	CookieSecureAlways = "always"
)

// Tokens holds the configured API tokens per scope. The values are secrets:
// Tokens implements fmt.Stringer, fmt.GoStringer, and slog.LogValuer so that
// printing or logging a Config (or a Tokens) shows only counts, never values.
type Tokens struct {
	Admin, Write, Read, Eval []string
}

// Count returns the total number of configured tokens across all scopes.
func (t Tokens) Count() int {
	return len(t.Admin) + len(t.Write) + len(t.Read) + len(t.Eval)
}

// String reports counts only; token values are never rendered.
func (t Tokens) String() string {
	return fmt.Sprintf("tokens{admin:%d write:%d read:%d eval:%d}",
		len(t.Admin), len(t.Write), len(t.Read), len(t.Eval))
}

// GoString makes %#v redact too.
func (t Tokens) GoString() string { return t.String() }

// LogValue makes slog render counts only.
func (t Tokens) LogValue() slog.Value { return slog.StringValue(t.String()) }

// AuthEnabled reports whether authentication is on: CONSOLE_AUTH is not
// "disabled" and at least one token is configured.
func (c Config) AuthEnabled() bool {
	return c.AuthMode != AuthDisabled && c.Tokens.Count() > 0
}

// loadTokens resolves one scope's tokens from NAME (comma-separated) and
// NAME_FILE (one per line). Setting both is a conflict and an error: silently
// preferring one would hide a stale secret source during rotation.
//
// Env list: split on commas only; each entry is trimmed of surrounding
// whitespace and empty entries are skipped. File: one token per line; each line
// is trimmed, blank lines and lines whose first non-space character is '#' are
// skipped (a '#' elsewhere in a line is part of the token).
func loadTokens(name string) ([]string, error) {
	env := strings.TrimSpace(os.Getenv(name))
	file := strings.TrimSpace(os.Getenv(name + "_FILE"))
	if env != "" && file != "" {
		return nil, fmt.Errorf("%s and %s_FILE are both set: set only one", name, name)
	}
	if file == "" {
		return splitComma(env), nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return nil, fmt.Errorf("%s_FILE %q: %w", name, file, err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func splitComma(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// loadAuth fills the auth fields of c from the environment. Errors are kept in
// c.authErr and surfaced by Validate so FromEnv keeps its signature.
func (c *Config) loadAuth() {
	c.AuthMode = strings.ToLower(strings.TrimSpace(os.Getenv("CONSOLE_AUTH")))
	if c.AuthMode == "" {
		c.AuthMode = AuthAuto
	}
	c.CookieSecure = strings.ToLower(strings.TrimSpace(os.Getenv("CONSOLE_COOKIE_SECURE")))
	if c.CookieSecure == "" {
		c.CookieSecure = CookieSecureAuto
	}
	var errs []error
	for _, s := range []struct {
		env string
		dst *[]string
	}{
		{"CONSOLE_ADMIN_TOKENS", &c.Tokens.Admin},
		{"CONSOLE_WRITE_TOKENS", &c.Tokens.Write},
		{"CONSOLE_READ_TOKENS", &c.Tokens.Read},
		{"CONSOLE_EVAL_TOKENS", &c.Tokens.Eval},
	} {
		toks, err := loadTokens(s.env)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		*s.dst = toks
	}
	c.authErr = errors.Join(errs...)
}

// validateAuth reports a bad CONSOLE_AUTH value or a token-source error.
func (c Config) validateAuth() error {
	switch c.AuthMode {
	case "", AuthAuto, AuthDisabled:
	default:
		return fmt.Errorf("invalid CONSOLE_AUTH %q: allowed values are %q or %q", c.AuthMode, AuthAuto, AuthDisabled)
	}
	switch c.CookieSecure {
	case "", CookieSecureAuto, CookieSecureAlways:
	default:
		return fmt.Errorf("invalid CONSOLE_COOKIE_SECURE %q: allowed values are %q or %q", c.CookieSecure, CookieSecureAuto, CookieSecureAlways)
	}
	return c.authErr
}
