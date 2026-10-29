package config

import (
	"strings"
	"testing"
)

func TestCookieSecureConfig(t *testing.T) {
	t.Setenv("CONSOLE_COOKIE_SECURE", "")
	if c := FromEnv(); c.CookieSecure != CookieSecureAuto || c.Validate() != nil {
		t.Fatalf("default = %q, %v", c.CookieSecure, c.Validate())
	}
	t.Setenv("CONSOLE_COOKIE_SECURE", "Always")
	if c := FromEnv(); c.CookieSecure != CookieSecureAlways || c.Validate() != nil {
		t.Fatalf("always = %q, %v", c.CookieSecure, c.Validate())
	}
	t.Setenv("CONSOLE_COOKIE_SECURE", "never")
	err := FromEnv().Validate()
	if err == nil || !strings.Contains(err.Error(), "CONSOLE_COOKIE_SECURE") {
		t.Fatalf("want CONSOLE_COOKIE_SECURE error, got %v", err)
	}
}
