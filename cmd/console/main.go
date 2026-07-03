// Command console is the Console CLI: a single static binary that serves the
// dashboard + API, manages feature flags and status components, and runs the
// onboarding assistant (Human and AI-Assisted modes).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/moosequest/console/internal/config"
)

// version is the release version. Release builds stamp it via
// -ldflags "-X main.version=<tag>" (see the Makefile / scripts/dist.sh). For an
// unstamped build it stays "dev", and resolveVersion falls back to the module
// version recorded by the Go toolchain (e.g. when installed with `go install
// …/cmd/console@vX.Y.Z`).
var version = "dev"

// resolveVersion returns the stamped version, or the module version from the
// build info when unstamped.
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

const usage = `Console — monitor and orchestrate your apps (feature flags + status).

Usage:
  console <command> [flags]

Commands:
  serve       Start the HTTP server (dashboard + API)
  flag        Manage feature flags (list, get, create, enable, disable, delete, eval)
  status      Manage status components (list, add, check, snapshot)
  onboard     Onboard an app into Console (Human or AI-Assisted mode)
  qr          Show a QR code to open the dashboard on your phone
  mcp         Serve Console over the Model Context Protocol (for AI agents)
  version     Print the version
  help        Show this help

Global environment:
  CONSOLE_ADDR           HTTP listen address (default "127.0.0.1:8080", loopback)
  CONSOLE_DB             SQLite path or DSN (default "console.db", "" for in-memory)
  CONSOLE_STORE_PLUGIN   path to a storage-backend plugin (e.g. console-plugin-postgres)
  CONSOLE_STATUS_PLUGINS comma/space-separated status-provider plugin paths
  CONSOLE_NOTIFY_PLUGINS comma/space-separated notifier plugin paths
  CONSOLE_LLM_PLUGIN     path to an LLM plugin (enables AI-Assisted onboarding)

Plugins inherit this environment, so provider-specific variables
(e.g. ANTHROPIC_API_KEY, CLOUDFLARE_API_TOKEN, CONSOLE_SLACK_WEBHOOK_URL) are
read by the relevant plugin.

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
	case "qr":
		return cmdQR(rest, cfg)
	case "mcp":
		return cmdMCP(rest, cfg)
	case "version", "--version", "-v":
		fmt.Printf("console %s\n", resolveVersion())
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
