package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestAuthToken(t *testing.T) {
	var out bytes.Buffer
	if err := runAuth(context.Background(), []string{"token"}, &out, authEnv{}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.HasPrefix(s, "csk_") || strings.Count(s, "\n") != 1 {
		t.Errorf("unexpected output %q", s)
	}
	if err := runAuth(context.Background(), []string{"bogus"}, &out, authEnv{}); err == nil {
		t.Error("want error for unknown subcommand")
	}
	if !strings.Contains(usage, "  auth ") {
		t.Error("usage must list auth")
	}
}

func TestAuthUsageDocumentsConfig(t *testing.T) {
	var b bytes.Buffer
	if err := runAuth(context.Background(), []string{"help"}, &b, authEnv{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Comma-separated", "trimmed", "_TOKENS_FILE", "CONSOLE_AUTH", "refuses to start"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("usage missing %q", want)
		}
	}
}
