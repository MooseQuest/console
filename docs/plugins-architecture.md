# Plugin architecture (out-of-process, gRPC)

Console plugins are **separate executables** that the `console` host launches as
subprocesses and talks to over gRPC, using
[hashicorp/go-plugin](https://github.com/hashicorp/go-plugin). This is the model
Terraform and Vault use. The benefits:

- **Drop-in, no recompile** — add a capability by placing a plugin binary and
  pointing config at it; the core binary is unchanged.
- **Isolation** — a plugin's dependencies (e.g. a Postgres driver) and crashes
  stay out of the core process, which remains a small static binary.
- **Polyglot** — because the contract is gRPC, a plugin can be written in any
  language, not just Go.

The core defines a gRPC contract per seam (`proto/`), a host-side **loader** that
launches a plugin and returns a value satisfying the seam's Go interface, and a
plugin-side **serve** helper. Engines never know an implementation lives in
another process.

## Seams

All four seams are out-of-process plugins:

| Seam | Interface | Plugin | Selected by |
|---|---|---|---|
| Storage | `store.Store` | `console-plugin-postgres` | `CONSOLE_STORE_PLUGIN` |
| Notify | `notify.Notifier` | `console-plugin-slack` | `CONSOLE_NOTIFY_PLUGINS` |
| Status | `status.Provider` | `console-plugin-cloudflare` | `CONSOLE_STATUS_PLUGINS` |
| LLM | `llm.Provider` | `console-plugin-anthropic` | `CONSOLE_LLM_PLUGIN` |

The **defaults stay built into the core** so it runs with zero plugins: SQLite
storage and the `http` status provider. Storage takes one plugin (it replaces
SQLite); notify and status take a list (each plugin is registered as a sink /
named provider); LLM takes one. Every plugin inherits the host's environment, so
provider-specific config (`CONSOLE_DB`, `CLOUDFLARE_API_TOKEN`,
`CONSOLE_SLACK_WEBHOOK_URL`, `ANTHROPIC_API_KEY`, `CONSOLE_MODEL`) reaches it.

## Using the Postgres store plugin

```bash
# 1. Build the host and the plugin
make build && make plugins        # -> ./console and ./bin/console-plugin-postgres

# 2. Point the host at the plugin + your Postgres DSN
export CONSOLE_STORE_PLUGIN=$PWD/bin/console-plugin-postgres
export CONSOLE_DB="postgres://user:pass@host:5432/console?sslmode=require"

./console serve
```

The host launches the plugin, performs the handshake, and uses it as the store
over gRPC. The plugin inherits the host's environment, so it reads `CONSOLE_DB`
itself. Without `CONSOLE_STORE_PLUGIN`, the host uses built-in SQLite.

## Using the Slack notifier plugin

Notifier plugins are listed in `CONSOLE_NOTIFY_PLUGINS` (comma/space-separated);
the host launches each and registers it as a sink. The Slack plugin reads its
webhook URL from the environment it inherits:

```bash
make build && make plugins        # -> ./bin/console-plugin-slack
export CONSOLE_NOTIFY_PLUGINS=$PWD/bin/console-plugin-slack
export CONSOLE_SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..."

./console serve
```

Now a component going down or a flag change is delivered to Slack by the plugin
subprocess. The `notify.Notifier` seam and the engines' event emission stay in
the core; only the sink is out-of-process.

## Using the status and LLM plugins

```bash
make build && make plugins   # -> ./bin/console-plugin-cloudflare, -anthropic

# Cloudflare Worker health as a status provider:
export CONSOLE_STATUS_PLUGINS=$PWD/bin/console-plugin-cloudflare
export CLOUDFLARE_API_TOKEN=...      # read by the plugin
# then add a component with provider "cloudflare-workers"

# AI-Assisted onboarding via Anthropic:
export CONSOLE_LLM_PLUGIN=$PWD/bin/console-plugin-anthropic
export ANTHROPIC_API_KEY=sk-ant-...  # read by the plugin (it exits if unset)
```

A status plugin registers itself as a named provider (its `Name()`), so a
component's `provider` field routes to it; the built-in `http` provider needs no
plugin. The LLM plugin, when set, powers AI-Assisted onboarding; with none, AI
mode is simply unavailable and Human mode still works.

## How it fits together

```
console (host)                         console-plugin-postgres (subprocess)
  internal/app.openStore                  main: postgres.Open(CONSOLE_DB)
        │ CONSOLE_STORE_PLUGIN set                     │
        ▼                                              ▼
  plugin.LoadStore(path) ──launch──▶ plugin.Serve(store) ──serves──▶ StoreService
        │  returns store.Store (gRPC client adapter)          (gRPC server adapter)
        ▼
  flags / status engines  (unchanged — they just see a store.Store)
```

- Contract: `proto/store.proto` → generated `internal/plugin/proto`.
- Host + plugin glue, handshake, and the client/server adapters: `internal/plugin`.
- The plugin executable: `cmd/console-plugin-postgres`.

Errors cross the boundary as gRPC status codes (`NotFound`, `AlreadyExists`) and
are mapped back to `core.ErrNotFound` / `core.ErrConflict`, so callers behave
identically to the in-process SQLite store.

## Writing a plugin

1. (If a new seam) define a gRPC service in `proto/` and run `make proto`.
2. Implement client + server adapters in `internal/plugin` that bridge the Go
   interface to the generated service (see the store adapters as the template).
3. Add a `cmd/console-plugin-<name>` that constructs your implementation and
   calls the seam's `Serve` helper.
4. Point the host at it via the seam's config (e.g. `CONSOLE_STORE_PLUGIN`).

Regenerating stubs needs `protoc` plus `protoc-gen-go` and `protoc-gen-go-grpc`
on `PATH`; the generated `*.pb.go` are committed so a normal build needs neither.
