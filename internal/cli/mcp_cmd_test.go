package cli

import (
	"strings"
	"testing"

	"github.com/moosequest/console/internal/config"
)

// The bearer token must never be accepted as a command-line flag (it would leak
// via process listings); only CONSOLE_TOKEN or -token-file.
func TestMCPHasNoTokenFlag(t *testing.T) {
	c := &cli{}
	err := c.cmdMCP([]string{"-addr", "127.0.0.1:1", "-token", "secret"}, config.Config{})
	if err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("want 'flag provided but not defined' error, got %v", err)
	}
}
