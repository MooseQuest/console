package main

import (
	"flag"
	"fmt"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/mcp"
)

const mcpUsage = `Serve Console over the Model Context Protocol (MCP), for AI agents.

Usage:
  console mcp [-write] [-addr host:port]

The server speaks MCP over stdio, so an MCP client (Claude Desktop, Claude Code,
or any MCP host) launches "console mcp" as a subprocess. It exposes tools to
list and evaluate feature flags and read component health; with -write it also
exposes tools to create, toggle, and delete flags and components.

By default the server opens the local store and drives the engines in-process
(no running server needed). With -addr it instead targets a running
"console serve" over its JSON API — useful for a shared or remote instance.

Flags:
  -write            enable the mutating tools (create/toggle/delete). Off by
                    default; Console has no built-in auth, so writes are opt-in.
  -addr host:port   target a running console server over HTTP instead of the
                    local store (e.g. 127.0.0.1:8080)

Example (Claude Desktop / Code MCP config):
  {"command": "console", "args": ["mcp"]}          # read-only, local store
  {"command": "console", "args": ["mcp", "-write"]} # allow writes
`

func cmdMCP(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	write := fs.Bool("write", false, "enable mutating tools (create/toggle/delete)")
	addr := fs.String("addr", "", "target a running console server over HTTP instead of the local store")
	fs.Usage = func() { fmt.Print(mcpUsage) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	opts := mcp.Options{Name: "console", Version: resolveVersion(), AllowWrites: *write}

	var backend mcp.Backend
	if *addr != "" {
		b, err := mcp.NewHTTPBackend(*addr)
		if err != nil {
			return err
		}
		backend = b
	} else {
		a, err := app.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer a.Close()
		backend = mcp.NewEngineBackend(a)
	}

	return mcp.Run(ctx, backend, opts)
}
