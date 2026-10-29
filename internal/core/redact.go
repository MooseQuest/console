package core

import (
	"fmt"
	"strings"
)

// SecretPlaceholder is the literal rendered in place of a secret component
// config value on every read path (API, dashboard, MCP, logs). Sending it back
// on update means "keep the stored value".
const SecretPlaceholder = "[set]"

// secretMarkers are the case-insensitive substrings that mark a config key as
// secret. The match is deliberately broad: fail-safe over precise.
var secretMarkers = []string{"token", "secret", "key", "password", "credential", "auth", "webhook",
	"dsn", "passwd", "pass", "pwd", "bearer", "cookie", "signature", "jwt", "private", "connection_string"}

// IsSecretConfigKey reports whether a component config key holds a secret,
// by case-insensitive substring match against the known markers.
func IsSecretConfigKey(key string) bool {
	k := strings.ToLower(key)
	for _, m := range secretMarkers {
		if strings.Contains(k, m) {
			return true
		}
	}
	return false
}

// RedactConfig returns a copy of cfg with every secret value replaced by
// SecretPlaceholder. Non-secret values are returned verbatim. A nil or empty
// map is returned as nil. The input is never modified.
func RedactConfig(cfg map[string]string) map[string]string {
	if len(cfg) == 0 {
		return nil
	}
	out := make(map[string]string, len(cfg))
	for k, v := range cfg {
		if IsSecretConfigKey(k) {
			out[k] = SecretPlaceholder
		} else {
			out[k] = v
		}
	}
	return out
}

// MergeSecretConfig prepares an incoming (update) config for storage. For each
// secret key whose incoming value is SecretPlaceholder or which is omitted, the
// previously stored value is kept; a placeholder with no stored value is
// dropped rather than persisted. Any other incoming value replaces the stored
// one. The inputs are never modified.
func MergeSecretConfig(stored, incoming map[string]string) map[string]string {
	out := make(map[string]string, len(incoming))
	for k, v := range incoming {
		if IsSecretConfigKey(k) && v == SecretPlaceholder {
			continue
		}
		out[k] = v
	}
	for k, v := range stored {
		if !IsSecretConfigKey(k) {
			continue
		}
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Redacted returns a copy of the component that is safe to emit: its Config has
// secret values replaced by SecretPlaceholder.
func (c Component) Redacted() Component {
	c.Config = RedactConfig(c.Config)
	return c
}

// RedactComponents returns redacted copies of every component in cs.
func RedactComponents(cs []Component) []Component {
	if cs == nil {
		return nil
	}
	out := make([]Component, len(cs))
	for i, c := range cs {
		out[i] = c.Redacted()
	}
	return out
}

// String renders the component with a redacted Config so that accidental
// formatting with %v or %+v (for example in logs) cannot leak secrets.
func (c Component) String() string {
	return fmt.Sprintf("{Key:%s Name:%s Provider:%s Config:%v}", c.Key, c.Name, c.Provider, RedactConfig(c.Config))
}

// GoString keeps %#v from bypassing redaction.
func (c Component) GoString() string { return c.String() }
