package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func clearAuthEnv(t *testing.T) {
	t.Helper()
	for _, s := range []string{"ADMIN", "WRITE", "READ", "EVAL"} {
		t.Setenv("CONSOLE_"+s+"_TOKENS", "")
		t.Setenv("CONSOLE_"+s+"_TOKENS_FILE", "")
	}
	t.Setenv("CONSOLE_AUTH", "")
}

func TestAuthDefaults(t *testing.T) {
	clearAuthEnv(t)
	c := FromEnv()
	if c.AuthMode != AuthAuto || c.AuthEnabled() || c.Validate() != nil {
		t.Fatalf("default: %+v", c)
	}
}

func TestAuthModeValidation(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("CONSOLE_AUTH", "Disabled")
	if c := FromEnv(); c.AuthMode != AuthDisabled || c.Validate() != nil {
		t.Fatalf("disabled: %+v", c)
	}
	t.Setenv("CONSOLE_AUTH", "off")
	err := FromEnv().Validate()
	if err == nil || !strings.Contains(err.Error(), "CONSOLE_AUTH") {
		t.Fatalf("want CONSOLE_AUTH error, got %v", err)
	}
}

func TestTokensFromEnv(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("CONSOLE_ADMIN_TOKENS", " a1 , ,a2,, a3 ")
	t.Setenv("CONSOLE_EVAL_TOKENS", "e1")
	c := FromEnv()
	if !reflect.DeepEqual(c.Tokens.Admin, []string{"a1", "a2", "a3"}) || !reflect.DeepEqual(c.Tokens.Eval, []string{"e1"}) {
		t.Fatalf("tokens: %#v", c.Tokens)
	}
	if !c.AuthEnabled() {
		t.Fatal("auth should be enabled with tokens")
	}
	t.Setenv("CONSOLE_AUTH", "disabled")
	if FromEnv().AuthEnabled() {
		t.Fatal("CONSOLE_AUTH=disabled must disable auth")
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTokensFromFile(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("CONSOLE_WRITE_TOKENS_FILE", writeFile(t, "# rotation\n\n  w1  \r\nw2\n   # indented comment\nw#3\n"))
	c := FromEnv()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"w1", "w2", "w#3"}; !reflect.DeepEqual(c.Tokens.Write, want) {
		t.Fatalf("got %#v want %v", c.Tokens.Write, want)
	}
}

func TestTokensEnvAndFileConflict(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("CONSOLE_READ_TOKENS", "r1")
	t.Setenv("CONSOLE_READ_TOKENS_FILE", writeFile(t, "r2\n"))
	err := FromEnv().Validate()
	if err == nil || !strings.Contains(err.Error(), "CONSOLE_READ_TOKENS") || strings.Contains(err.Error(), "r1") {
		t.Fatalf("want conflict error without token values, got %v", err)
	}
}

func TestTokensFileMissing(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("CONSOLE_ADMIN_TOKENS_FILE", filepath.Join(t.TempDir(), "nope"))
	if err := FromEnv().Validate(); err == nil || !strings.Contains(err.Error(), "CONSOLE_ADMIN_TOKENS_FILE") {
		t.Fatalf("want file error, got %v", err)
	}
}

func TestTokensNeverRendered(t *testing.T) {
	clearAuthEnv(t)
	const secret = "csk_supersecretvalue"
	t.Setenv("CONSOLE_ADMIN_TOKENS", secret)
	c := FromEnv()
	for _, out := range []string{
		fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c),
		fmt.Sprintf("%s", c.Tokens), c.Tokens.LogValue().String(),
		slog.GroupValue(slog.Any("cfg", c.Tokens)).String(),
	} {
		if strings.Contains(out, secret) {
			t.Fatalf("token leaked in %q", out)
		}
	}
}
