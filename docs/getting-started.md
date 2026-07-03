# Getting started

Console is a single static Go binary that combines feature flags and status
monitoring. This guide takes you from a clean checkout to a running dashboard:
install → first flag → first check → serve → onboard.

- [Install](#install)
- [Configure](#configure)
- [Your first flag](#your-first-flag)
- [Your first status check](#your-first-status-check)
- [Serve the dashboard + API](#serve-the-dashboard--api)
- [Onboard an app](#onboard-an-app)
- [Where to go next](#where-to-go-next)

## Install

**Prebuilt binary (no Go toolchain).** Grab the bundle for your platform from the
[latest release](https://github.com/MooseQuest/console/releases/latest); each
bundle ships the `console` binary plus all plugins.

```bash
# macOS / Linux (pick your os/arch: darwin|linux, arm64|amd64)
curl -sSLO https://github.com/MooseQuest/console/releases/download/v0.5.0/console_v0.5.0_darwin_arm64.tar.gz
curl -sSLO https://github.com/MooseQuest/console/releases/download/v0.5.0/SHA256SUMS.txt
shasum -a 256 -c SHA256SUMS.txt --ignore-missing   # verify integrity
tar xzf console_v0.5.0_darwin_arm64.tar.gz && cd console_v0.5.0_darwin_arm64
```

**From source.** Console needs **Go 1.25.11+** to build. There is no cgo dependency —
the embedded database is the pure-Go
[`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite), so the result is a
truly static binary.

```bash
git clone https://github.com/moosequest/console
cd console

make build            # produces ./console
# equivalently:
go build -o console ./cmd/console
```

Verify the build:

```bash
./console version
```

## Configure

All configuration is via environment variables; per-command CLI flags override
them. Sensible defaults mean you usually need none of these to start.

| Variable | Default | Purpose |
|---|---|---|
| `CONSOLE_ADDR` | `127.0.0.1:8080` | HTTP listen address (loopback) |
| `CONSOLE_DB` | `console.db` | SQLite path / DSN (`""` for in-memory) |
| `CONSOLE_LLM_PLUGIN` | — | Path to an LLM plugin binary for AI-Assisted onboarding; unset = AI mode off |
| `CONSOLE_MODEL` | provider default | LLM model override |
| `ANTHROPIC_API_KEY` | — | API key for the Anthropic provider |

> An in-memory database (`CONSOLE_DB=""`) is handy for experiments and tests —
> nothing is persisted between runs.

## Your first flag

Create a boolean flag scoped to your beta audience, rolled out to half of them:

```bash
./console flag create new-dashboard \
  -desc "New dashboard UI" \
  -scope beta \
  -rollout 50 \
  -enabled
```

Evaluate it for a subject. Because beta scope keys off subject attributes, pass
the audience attribute:

```bash
./console flag eval new-dashboard -subject user-123 -attr audience=beta
# → enabled=true variant=on reason=rollout_included   (or rollout_excluded for ~half of subjects)
```

Evaluation is **deterministic** — the same `(flag, subject)` pair always returns
the same result — so trying a few subject IDs shows a stable ~50% split.

List and inspect:

```bash
./console flag list
./console flag get new-dashboard
```

See the [flags guide](flags.md) for scopes, multivariate flags, and the full
evaluation pipeline.

## Your first status check

Register a component to monitor and run a check against it:

```bash
./console status add api -url https://example.com/health -name "Public API"
./console status check api
# → operational | degraded | down | unknown, with latency and a message
```

Aggregate the latest check per component into one health snapshot:

```bash
./console status snapshot
```

See the [status guide](status.md) for providers, component config, and how the
snapshot aggregation handles unchecked components.

## Serve the dashboard + API

```bash
./console serve                 # listens on 127.0.0.1:8080 (loopback) by default
./console serve -addr :9090     # override the address
./console serve -db ./my.db     # override the database path
```

Open <http://localhost:8080> for the dashboard (Overview, Flags, Status). The
same data is available as JSON under `/api/*` — the dashboard is just a client
of that API. See the [API reference](api.md).

```bash
curl localhost:8080/api/health
curl localhost:8080/api/flags
```

> **On your phone:** bind off loopback and print a QR to scan from a device on
> the same Wi-Fi: `CONSOLE_ADDR=:8080 ./console qr`. This exposes the
> unauthenticated dashboard to your LAN — only do it on a trusted network.

## Onboard an app

Onboarding builds a **plan** — the components to monitor and flags to create for
one application — then optionally applies it and writes a Markdown setup guide.

```bash
# Human mode — an interactive wizard
./console onboard

# AI-Assisted mode — Claude drafts the plan from a description
export CONSOLE_LLM_PLUGIN=$PWD/bin/console-plugin-anthropic
export ANTHROPIC_API_KEY=sk-ant-...
./console onboard -ai \
  -name "Acme" \
  -desc "A Rails store with a Sidekiq worker and a Postgres DB" \
  -apply \
  -guide acme-setup.md
```

`-apply` writes the plan into the store; re-running converges (existing keys are
skipped, not errors). `-guide <path>` renders the plan as Markdown. See the
[onboarding guide](onboarding.md).

> AI mode is enabled only when `CONSOLE_LLM_PLUGIN` points at an LLM plugin
> binary (`console-plugin-anthropic`, `-openai`, or `-ollama`; build them with
> `make plugins`). Leave it unset to use Human mode.

## Where to go next

- [Feature flags](flags.md) — scopes, rollout, determinism, multivariate.
- [Status monitoring](status.md) — components, providers, snapshots.
- [HTTP API](api.md) — integrate your apps (curl + `fetch` examples).
- [MCP server](mcp.md) — operate Console from an AI agent (`console mcp`).
- [Architecture](architecture.md) — how the pieces fit together.
- [Plugin architecture](plugins-architecture.md) — add a storage / status / notify / LLM backend.
