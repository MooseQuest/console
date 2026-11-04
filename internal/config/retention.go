package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultAuditRetentionDays is how long audit events are kept when
	// CONSOLE_AUDIT_RETENTION_DAYS is unset.
	DefaultAuditRetentionDays = 90
	// MaxAuditRetentionDays bounds CONSOLE_AUDIT_RETENTION_DAYS (about 100
	// years) so the day count can never overflow a time.Duration.
	MaxAuditRetentionDays = 36500
)

// AuditRetention returns the audit retention window, or 0 when events are kept
// forever (AuditRetentionDays == 0).
func (c Config) AuditRetention() time.Duration {
	return time.Duration(c.AuditRetentionDays) * 24 * time.Hour
}

// ParseAuditRetentionDays parses a CONSOLE_AUDIT_RETENTION_DAYS value. An empty
// (or all-blank) value yields the default; "0" means keep forever. Anything
// non-numeric, negative, or above MaxAuditRetentionDays is an error naming the
// variable.
func ParseAuditRetentionDays(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultAuditRetentionDays, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid CONSOLE_AUDIT_RETENTION_DAYS %q: must be a whole number of days (0 keeps audit events forever)", raw)
	}
	if n < 0 || n > MaxAuditRetentionDays {
		return 0, fmt.Errorf("invalid CONSOLE_AUDIT_RETENTION_DAYS %q: must be between 0 and %d (0 keeps audit events forever)", raw, MaxAuditRetentionDays)
	}
	return n, nil
}

// loadRetention fills AuditRetentionDays from the environment. A bad value is
// kept in c.retentionErr and surfaced by Validate so FromEnv keeps its
// signature.
func (c *Config) loadRetention() {
	n, err := ParseAuditRetentionDays(os.Getenv("CONSOLE_AUDIT_RETENTION_DAYS"))
	if err != nil {
		c.retentionErr = err
		return
	}
	c.AuditRetentionDays = n
}
