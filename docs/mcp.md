# MCP server

Console speaks the [Model Context Protocol](https://modelcontextprotocol.io)
(MCP), so an AI agent — Claude Desktop, Claude Code, or any MCP host — can
operate a Console instance directly: list and evaluate feature flags, read
component health, and (opt-in) create, toggle, and delete flags and components.

MCP is a **consumer surface**, like the CLI and the dashboard — not one of
Console's plugin seams (store/status/notify/llm). The server is the `console mcp`
subcommand; it speaks MCP over **stdio**, so the MCP host launches it as a
subprocess.

## Quick start

Build the binary (`make build`) and point your MCP client at it. For Claude
Desktop / Claude Code, add Console to the MCP servers config:

```json
{
  "mcpServers": {
    "console": {
      "command": "console",
      "args": ["mcp"]
    }
  }
}
```

That runs a **read-only** server (flags, components, evaluation, health) backed
by the local store. To allow the agent to make changes, add `-write`:

```json
{
  "mcpServers": {
    "console": {
      "command": "console",
      "args": ["mcp", "-write"],
      "env": { "CONSOLE_DB": "/path/to/console.db" }
    }
  }
}
```

The server inherits the host environment, so the same `CONSOLE_*` variables the
CLI uses (`CONSOLE_DB`, `CONSOLE_STORE_PLUGIN`, `CONSOLE_STATUS_PLUGINS`, …)
configure it. Without `CONSOLE_DB` it uses the default `console.db` in the working
directory.

## Backend: local store vs. a running server

By default `console mcp` opens the store and drives the engines **in-process**,
exactly as the CLI does — no running server required, works offline.

Pass `-addr` to instead target a running `console serve` over its JSON API,
useful for a shared or remote instance:

```bash
console mcp -addr 127.0.0.1:8080
```

> **Auth.** Console has no built-in authentication. In `-addr` mode point at a
> loopback address or an authenticating proxy; see
> [security/runtime-hardening.md](security/runtime-hardening.md). In the default
> in-process mode the trust boundary is simply the local user who launched the
> subprocess.

## Tools

Read tools are always registered. Write tools are registered only with `-write`;
`delete_*` are additionally annotated destructive so clients prompt before
running them.

| Tool | Class | Description |
|---|---|---|
| `list_flags` | read | List all feature flags. |
| `get_flag` | read | Get one flag by key (incl. variants). |
| `evaluate_flag` | read | Evaluate a flag for a subject (`key`, `subject`, optional `attributes`). Deterministic per (flag, subject). |
| `list_components` | read | List monitored components. |
| `get_component` | read | Get one component by key. |
| `health_snapshot` | read | Aggregate health across components (worst-wins). |
| `check_component` | read | Run one component's health check now. |
| `create_flag` | write | Create a flag (`key`, `scope`, `rollout`, `enabled`, …). |
| `set_flag_enabled` | write | Enable or disable a flag. |
| `delete_flag` | write · destructive | Delete a flag. |
| `add_component` | write | Add a component to monitor (`key`, `provider`, `config`). |
| `delete_component` | write · destructive | Delete a component. |

## Resources & prompts

- **Resources** — `console://health` (the live snapshot) and `console://flags`
  (the flag catalog), both `application/json`, for ambient context.
- **Prompt** — `onboard` (arguments: `app`, optional `description`) drafts a
  starter set of flags and components and walks the agent through creating them
  with the write tools.

## Design notes

- Tool input schemas are inferred from typed Go structs (via the SDK's
  `jsonschema` struct tags), so the wire schema and the handler signature can
  never drift.
- Tool results are the same JSON shapes the [HTTP API](api.md) returns; health
  and check results additionally render their state as a readable string
  (`operational`/`degraded`/`down`/`unknown`) rather than the raw integer.
- The server is built on the official
  [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk),
  which is pure Go — the Console binary stays cgo-free and statically linkable.
