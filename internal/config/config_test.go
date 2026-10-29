package config

import (
	"strings"
	"testing"
)

func TestLogFormat(t *testing.T) {
	t.Setenv("CONSOLE_LOG_FORMAT", "")
	if c := FromEnv(); c.LogFormat != "text" || c.Validate() != nil {
		t.Fatalf("default: %+v", c)
	}
	t.Setenv("CONSOLE_LOG_FORMAT", "JSON")
	if c := FromEnv(); c.LogFormat != "json" || c.Validate() != nil {
		t.Fatalf("json: %+v", c)
	}
	t.Setenv("CONSOLE_LOG_FORMAT", "xml")
	err := FromEnv().Validate()
	if err == nil || !strings.Contains(err.Error(), `"text"`) || !strings.Contains(err.Error(), `"json"`) {
		t.Fatalf("want error naming allowed values, got %v", err)
	}
}
