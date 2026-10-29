package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestAuthToken(t *testing.T) {
	var out bytes.Buffer
	if err := runAuth([]string{"token"}, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.HasPrefix(s, "csk_") || strings.Count(s, "\n") != 1 {
		t.Errorf("unexpected output %q", s)
	}
	if err := runAuth([]string{"bogus"}, &out); err == nil {
		t.Error("want error for unknown subcommand")
	}
	if !strings.Contains(usage, "  auth ") {
		t.Error("usage must list auth")
	}
}
