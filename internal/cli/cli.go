// Package cli implements the Console CLI: a single static binary that serves the
// dashboard + API, manages feature flags and status components, and runs the
// onboarding assistant (Human and AI-Assisted modes).
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

// Options configures Main. A second main package (for example an OTel-enabled
// build) reuses the CLI by supplying its own Options.
type Options struct {
	// Version is the release version, typically stamped by the calling main
	// via -ldflags "-X main.version=<tag>". Empty or "dev" falls back to the
	// module version recorded by the Go toolchain (e.g. `go install
	// …/cmd/console@vX.Y.Z`).
	Version string
	// Recorder builds the instrumentation Recorder for a config. Nil selects
	// app.DefaultRecorder (obs.Nop, or an in-memory registry when cfg.Metrics).
	Recorder func(cfg config.Config) obs.Recorder
}

// cli carries the Options through the subcommands.
type cli struct{ opts Options }

// appOpts translates the CLI options into app.New options.
func (c *cli) appOpts() []app.Option {
	if c.opts.Recorder == nil {
		return nil
	}
	return []app.Option{app.WithRecorder(c.opts.Recorder)}
}

// resolveVersion returns the stamped version, or the module version from the
// build info when unstamped.
func (c *cli) resolveVersion() string {
	v := c.opts.Version
	if v == "" {
		v = "dev"
	}
	if v != "dev" {
		return v
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if mv := info.Main.Version; mv != "" && mv != "(devel)" {
			return mv
		}
	}
	return v
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

// Main runs the Console CLI with os.Args and returns the process exit code
// (0 on success, 1 on error). The caller passes it to os.Exit.
func Main(opts Options) int {
	c := &cli{opts: opts}
	if err := c.run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "console: "+err.Error())
		return 1
	}
	return 0
}

func (c *cli) run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	cmd, rest := args[0], args[1:]
	cfg := config.FromEnv()

	switch cmd {
	case "serve":
		return c.cmdServe(rest, cfg)
	case "flag", "flags":
		return c.cmdFlag(rest, cfg)
	case "status":
		return c.cmdStatus(rest, cfg)
	case "onboard":
		return c.cmdOnboard(rest, cfg)
	case "qr":
		return c.cmdQR(rest, cfg)
	case "mcp":
		return c.cmdMCP(rest, cfg)
	case "version", "--version", "-v":
		fmt.Printf("console %s\n", c.resolveVersion())
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
