// Command console is the Console CLI: a single static binary that serves the
// dashboard + API, manages feature flags and status components, and runs the
// onboarding assistant (Human and AI-Assisted modes).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/moosequest/console/internal/config"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const usage = `Console — monitor and orchestrate your apps (feature flags + status).

Usage:
  console <command> [flags]

Commands:
  serve       Start the HTTP server (dashboard + API)
  flag        Manage feature flags (list, get, create, enable, disable, delete, eval)
  status      Manage status components (list, add, check, snapshot)
  onboard     Onboard an app into Console (Human or AI-Assisted mode)
  version     Print the version
  help        Show this help

Global environment:
  CONSOLE_ADDR          HTTP listen address (default ":8080")
  CONSOLE_DB            SQLite path or DSN (default "console.db", "" for in-memory)
  CONSOLE_LLM_PROVIDER  AI provider for onboarding (default "anthropic", "" to disable)
  CONSOLE_MODEL         LLM model override
  ANTHROPIC_API_KEY     API key for the Anthropic provider

Run "console <command> -h" for command-specific flags.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "console: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	cmd, rest := args[0], args[1:]
	cfg := config.FromEnv()

	switch cmd {
	case "serve":
		return cmdServe(rest, cfg)
	case "flag", "flags":
		return cmdFlag(rest, cfg)
	case "status":
		return cmdStatus(rest, cfg)
	case "onboard":
		return cmdOnboard(rest, cfg)
	case "version", "--version", "-v":
		fmt.Printf("console %s\n", version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q (run \"console help\")", cmd)
	}
}

// signalContext returns a context cancelled on SIGINT/SIGTERM, for graceful
// shutdown of long-running commands.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// leadingArg peels a leading positional argument (the <key> in
// "console flag create <key> -flags...") off the front of args, returning it
// and the remaining flag arguments. Go's flag package stops at the first
// non-flag token, so the key must be separated out before parsing. Returns an
// empty key when args is empty or starts with a flag.
func leadingArg(args []string) (key string, rest []string) {
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		return args[0], args[1:]
	}
	return "", args
}
