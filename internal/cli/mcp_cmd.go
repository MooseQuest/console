package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/mcp"
)

const mcpUsage = `Serve Console over the Model Context Protocol (MCP), for AI agents.

Usage:
  console mcp [-write] [-addr host:port]
                      [-token-file path] [-insecure-token]

The server speaks MCP over stdio, so an MCP client (Claude Desktop, Claude Code,
or any MCP host) launches "console mcp" as a subprocess. It exposes tools to
list and evaluate feature flags and read component health; with -write it also
exposes tools to create, toggle, and delete flags and components.

By default the server opens the local store and drives the engines in-process
(no running server needed). With -addr it instead targets a running
"console serve" over its JSON API — useful for a shared or remote instance.

Flags:
  -write            enable the mutating tools (create/toggle/delete). Off by
                    default; writes are opt-in.
  -addr host:port   target a running console server over HTTP instead of the
                    local store (e.g. 127.0.0.1:8080)
  -token-file path  with -addr: read the bearer token from this file (trailing
                    newline trimmed). Takes precedence over CONSOLE_TOKEN. There
                    is deliberately no -token flag (it would leak via ps).
  -insecure-token   with -addr: allow sending the token over plain http:// to a
                    non-loopback host (default: refused)

Environment:
  CONSOLE_TOKEN     bearer token for -addr mode. Use a read token for read-only
                    mode and a write token with -write.

Example (Claude Desktop / Code MCP config):
  {"command": "console", "args": ["mcp"]}          # read-only, local store
  {"command": "console", "args": ["mcp", "-write"]} # allow writes
`

func (c *cli) cmdMCP(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	write := fs.Bool("write", false, "enable mutating tools (create/toggle/delete)")
	addr := fs.String("addr", "", "target a running console server over HTTP instead of the local store")
	tokenFile := fs.String("token-file", "", "with -addr: file containing the bearer token")
	insecure := fs.Bool("insecure-token", false, "with -addr: allow a token over plain http to a non-loopback host")
	fs.Usage = func() { fmt.Print(mcpUsage) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	opts := mcp.Options{Name: "console", Version: c.resolveVersion(), AllowWrites: *write}

	var backend mcp.Backend
	if *addr != "" {
		tok, err := mcp.ResolveToken(os.Getenv("CONSOLE_TOKEN"), *tokenFile)
		if err != nil {
			return err
		}
		b, err := mcp.NewHTTPBackend(*addr, mcp.WithToken(tok), mcp.WithInsecureToken(*insecure))
		if err != nil {
			return err
		}
		backend = b
	} else {
		if *tokenFile != "" || *insecure {
			return fmt.Errorf("mcp: -token-file and -insecure-token require -addr")
		}
		a, err := app.New(ctx, cfg, c.appOpts()...)
		if err != nil {
			return err
		}
		defer a.Close()
		backend = mcp.NewEngineBackend(a)
	}

	return mcp.Run(ctx, backend, opts)
}
