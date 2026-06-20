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
another process. The host runs go-plugin with **AutoMTLS enabled**
(`internal/plugin/host.go`), so each launch generates a per-launch mutual-TLS
certificate and the host talks only to the subprocess it started.

## Seams

All four seams are out-of-process plugins, and several now have more than one
plugin to choose from:

| Seam | Interface | Selected by | Selection shape |
|---|---|---|---|
| Storage | `store.Store` | `CONSOLE_STORE_PLUGIN` | one path (replaces built-in SQLite) |
| Status | `status.Provider` | `CONSOLE_STATUS_PLUGINS` | comma/space list of paths |
| Notify | `notify.Notifier` | `CONSOLE_NOTIFY_PLUGINS` | comma/space list of paths |
| LLM | `llm.Provider` | `CONSOLE_LLM_PLUGIN` | one path (one provider at a time) |

The **defaults stay built into the core** so it runs with zero plugins: SQLite
storage and the `http` status provider. Storage takes one plugin (it replaces
SQLite); status and notify take a **list** (each plugin is registered as a named
provider / sink); LLM takes **one** (you pick which provider). Every plugin
inherits the host's environment, so provider-specific config reaches it.

## Plugin catalog

Ten plugins ship today. Each is a `console-plugin-*` binary built from
`cmd/console-plugin-*`; point the seam's selection variable at its path. The
"Reads" column lists the provider-specific config the plugin reads from the
host's environment or, for status providers, from the component's `config` map.

| Binary | Seam | Selected by | Reads |
|---|---|---|---|
| `console-plugin-postgres` | store | `CONSOLE_STORE_PLUGIN` | `CONSOLE_DB` (a `postgres://` DSN) |
| `console-plugin-cloudflare` | status (`cloudflare-workers`) | `CONSOLE_STATUS_PLUGINS` | component config `account_id`, `worker`, `api_token` (default `CLOUDFLARE_API_TOKEN`), `window`, `degraded_pct`, `down_pct` |
| `console-plugin-heroku` | status (`heroku`) | `CONSOLE_STATUS_PLUGINS` | component config `app`, `api_token` (default `HEROKU_API_KEY`), `timeout` |
| `console-plugin-sentry` | status (`sentry`) | `CONSOLE_STATUS_PLUGINS` | component config `org`, `project`, `auth_token` (default `SENTRY_AUTH_TOKEN`), `degraded_count` (1), `down_count` (10), `timeout` |
| `console-plugin-slack` | notify | `CONSOLE_NOTIFY_PLUGINS` | `CONSOLE_SLACK_WEBHOOK_URL` |
| `console-plugin-webhook` | notify | `CONSOLE_NOTIFY_PLUGINS` | `CONSOLE_WEBHOOK_URL` (required), `CONSOLE_WEBHOOK_SECRET` (optional; sent as `X-Webhook-Secret`) |
| `console-plugin-email` | notify | `CONSOLE_NOTIFY_PLUGINS` | `SMTP_HOST` (req), `SMTP_PORT` (587), `SMTP_USERNAME`, `SMTP_PASSWORD`, `EMAIL_FROM` (req), `EMAIL_TO` (req, comma-separated) |
| `console-plugin-anthropic` | llm | `CONSOLE_LLM_PLUGIN` | `ANTHROPIC_API_KEY` (req), `CONSOLE_MODEL` (opt) |
| `console-plugin-openai` | llm | `CONSOLE_LLM_PLUGIN` | `OPENAI_API_KEY` (req), `CONSOLE_MODEL` (opt; default `gpt-4o-mini`) |
| `console-plugin-ollama` | llm | `CONSOLE_LLM_PLUGIN` | `OLLAMA_HOST` (opt; default `http://localhost:11434`), `CONSOLE_MODEL` (opt; default `llama3.1`; no API key) |

Notes on the status providers: `heroku` maps a dyno's state to
operational/degraded/down; `sentry` maps the unresolved-issue count for a
project (`< degraded_count` operational, `≥ down_count` down). For the LLM seam,
`ollama` needs no API key and talks to a local Ollama server, which makes it the
easy fully-local option for AI-Assisted onboarding.

> **Build first.** All the examples below assume you have built the host and the
> plugin binaries once: `make build && make plugins` (→ `./console` and
> `./bin/console-plugin-*`). The `export` lines then point the host at the
> plugins you want.

## Using the Postgres store plugin

```bash
# Point the host at the plugin + your Postgres DSN
export CONSOLE_STORE_PLUGIN=$PWD/bin/console-plugin-postgres
export CONSOLE_DB="postgres://user:pass@host:5432/console?sslmode=require"

./console serve
```

The host launches the plugin, performs the handshake, and uses it as the store
over gRPC. The plugin inherits the host's environment, so it reads `CONSOLE_DB`
itself. Without `CONSOLE_STORE_PLUGIN`, the host uses built-in SQLite.

## Using the notifier plugins

Notifier plugins are listed in `CONSOLE_NOTIFY_PLUGINS` (comma/space-separated);
the host launches each and registers it as a sink, so you can run several at
once. Each reads its own config from the environment it inherits:

```bash
# Run all three sinks at once:
export CONSOLE_NOTIFY_PLUGINS="$PWD/bin/console-plugin-slack,$PWD/bin/console-plugin-webhook,$PWD/bin/console-plugin-email"

# Slack — posts to an Incoming Webhook:
export CONSOLE_SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..."

# Webhook — POSTs each event as JSON; optional shared secret sent as X-Webhook-Secret:
export CONSOLE_WEBHOOK_URL="https://example.com/console-events"
export CONSOLE_WEBHOOK_SECRET="s3cr3t"   # optional

# Email — SMTP; FROM/TO required, TO is comma-separated:
export SMTP_HOST=smtp.example.com        # SMTP_PORT defaults to 587
export SMTP_USERNAME=... SMTP_PASSWORD=...
export EMAIL_FROM="console@example.com"
export EMAIL_TO="oncall@example.com,ops@example.com"

./console serve
```

Now a component going down or a flag change is delivered to every configured
sink by its plugin subprocess. The `notify.Notifier` seam and the engines' event
emission stay in the core; only the sinks are out-of-process.

## Using the status and LLM plugins

```bash
# Status providers (a list — each registers under its own name):
export CONSOLE_STATUS_PLUGINS="$PWD/bin/console-plugin-cloudflare,$PWD/bin/console-plugin-heroku,$PWD/bin/console-plugin-sentry"
export CLOUDFLARE_API_TOKEN=...   # default token for the cloudflare-workers provider
export HEROKU_API_KEY=...         # default token for the heroku provider
export SENTRY_AUTH_TOKEN=...      # default token for the sentry provider
# then add components with provider "cloudflare-workers", "heroku", or "sentry"

# LLM — pick exactly one provider:
export CONSOLE_LLM_PLUGIN=$PWD/bin/console-plugin-anthropic   # or -openai, or -ollama
export ANTHROPIC_API_KEY=sk-ant-...   # anthropic: required (it exits if unset)
# openai: OPENAI_API_KEY required (default model gpt-4o-mini)
# ollama: no API key — OLLAMA_HOST defaults to http://localhost:11434, model llama3.1
```

A status plugin registers itself as a named provider (its `Name()`), so a
component's `provider` field routes to it; the built-in `http` provider needs no
plugin. The `heroku` provider maps a dyno's state, and `sentry` maps a project's
unresolved-issue count, to operational/degraded/down. The LLM seam takes one
provider at a time (`CONSOLE_LLM_PLUGIN`) and powers AI-Assisted onboarding;
with none set, AI mode is simply unavailable and Human mode still works.

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
