<div align="center">

# Console

**A modular, self-hostable control plane for the apps you build — feature flags + status monitoring in one small binary.**

[![CI](https://github.com/MooseQuest/console/actions/workflows/ci.yml/badge.svg)](https://github.com/MooseQuest/console/actions/workflows/ci.yml)
[![Release](https://img.shields.io/badge/release-v0.3.1-blue)](https://github.com/MooseQuest/console/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/moosequest/console)](https://goreportcard.com/report/github.com/moosequest/console)
[![Go](https://img.shields.io/github/go-mod/go-version/MooseQuest/console)](go.mod)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)

[Quickstart](#quickstart) · [Concepts](#concepts) · [CLI](#cli) · [HTTP API](#http-api) · [Onboarding](#onboarding-human--ai-assisted) · [Docs](./docs)

</div>

---

Console is a one-stop control plane for your application: ship features safely behind **feature flags**, and watch your services with **status checks** — from a single dashboard, a JSON API, and a CLI. It runs as **one static Go binary** with an embedded SQLite database, so getting started is `./console serve`.

It ships with two ways to get an app set up:

- **Human mode** — an interactive wizard walks you through what to monitor and which flags to create.
- **AI-Assisted mode** — describe your app in a sentence and an LLM (Claude by default) drafts the plan for you.

> Status: **v0.3.1**. The core engine, API, dashboard, CLI, and onboarding are working and tested. Interfaces may still change before v1.

## Why Console

- **One binary, no dependencies.** Pure-Go SQLite (no cgo) means a truly static binary you can drop on any host. Point it at Postgres later when you outgrow a single node.
- **Modular by design.** Four seams — storage, status providers, notifiers, and LLM providers — are **out-of-process plugins** the host launches over gRPC, so you add or swap a capability by dropping a `console-plugin-*` binary, no core recompile.
- **API-first.** The dashboard is just a client of the same HTTP API your apps and SDKs use.
- **Deterministic flag evaluation.** The same `(flag, subject)` always resolves the same way, with a stable percentage rollout you can reason about.

## Screenshots

| Overview | Flags | Status |
|---|---|---|
| ![Overview](docs/assets/overview.png) | ![Flags](docs/assets/flags.png) | ![Status](docs/assets/status.png) |

## Install

**Prebuilt binaries** (no Go toolchain needed) — grab the bundle for your platform
from the [latest release](https://github.com/MooseQuest/console/releases/latest).
Each bundle contains the `console` binary **plus all plugins**; the core runs with
zero plugins and zero config (embedded SQLite + the `http` status provider).

```bash
# macOS / Linux (pick your os/arch: darwin|linux, arm64|amd64)
curl -sSLO https://github.com/MooseQuest/console/releases/download/v0.3.1/console_v0.3.1_darwin_arm64.tar.gz
curl -sSLO https://github.com/MooseQuest/console/releases/download/v0.3.1/SHA256SUMS.txt
shasum -a 256 -c SHA256SUMS.txt --ignore-missing   # verify integrity
tar xzf console_v0.3.1_darwin_arm64.tar.gz && cd console_v0.3.1_darwin_arm64
./console serve                                     # http://127.0.0.1:8080
```

> **macOS:** downloaded binaries are quarantined by Gatekeeper. Clear it with
> `xattr -dr com.apple.quarantine ./console` (the binaries are not yet notarized).

**Windows (PowerShell):** download `console_v0.3.1_windows_amd64.zip` from the release,
verify against `SHA256SUMS.txt`, expand it, then run `.\console.exe serve`.

**From source** (needs Go 1.25.11+): `make build` (see [Quickstart](#quickstart)).

`console serve` binds to **loopback** by default (no built-in auth yet — see
[SECURITY.md](SECURITY.md)). To use a plugin (e.g. Postgres), point the matching
env var at its binary — see [docs/plugins-architecture.md](docs/plugins-architecture.md).

### Open it on your phone

To view the dashboard on your phone, run Console so your LAN can reach it, then
scan a QR code from the terminal:

```bash
CONSOLE_ADDR=:8080 ./console serve --qr   # bind to the LAN + print a QR to scan
# or, against a running server:
CONSOLE_ADDR=:8080 ./console qr
```

`qr` encodes `http://<your machine's LAN IP>:8080`; scan it from a phone on the
**same Wi-Fi**. (Binding off loopback exposes the unauthenticated dashboard to
your network — only do this on a trusted LAN.)

For access from **anywhere**, run a tunnel and QR-encode its URL — the tunnel's
own access controls are your authentication until Console's lands:

```bash
cloudflared tunnel --url http://127.0.0.1:8080      # -> https://<name>.trycloudflare.com
./console qr -url https://<name>.trycloudflare.com
```

## Quickstart

```bash
# 1. Build (needs Go 1.25.11+)
make build            # or: go build -o console ./cmd/console

# 2. Create a flag and evaluate it for a user
./console flag create new-dashboard -desc "New dashboard UI" -scope beta -rollout 50 -enabled
./console flag eval   new-dashboard -subject user-123 -attr audience=beta

# 3. Add a service to monitor and check it
./console status add api -url https://example.com/health -name "Public API"
./console status check api

# 4. Start the dashboard + API
./console serve       # http://localhost:8080
```

## Concepts

### Feature flags

A flag has a **scope** (the audience it applies to) and a **rollout** (the % of in-scope subjects who get it). Evaluation is deterministic per subject.

| Scope | In scope when… |
|---|---|
| `all` | always |
| `beta` / `alpha` | subject attribute `audience` equals the scope, or attribute `beta`/`alpha` == `"true"` |
| `cohort` | subject attribute `cohort` equals the flag's `cohort` |
| `experiment` | always in scope; the linked `experiment` is analysis metadata |

Boolean flags resolve to `on`/`off`. Multivariate flags carry weighted `variants` and resolve to one of them, deterministically by weight.

### Status

A **component** is a monitored part of your app (an API, a worker, a database), checked by a named **provider**. The built-in `http` provider probes a URL:

- `2xx` (or a configured `expect_status`) → **operational**
- any other HTTP response → **degraded**
- connection error / timeout → **down**

Providers are pluggable. Beyond the built-in `http` provider, status plugins add named providers: **`console-plugin-cloudflare`** (`cloudflare-workers` — Worker error rate from the Cloudflare GraphQL API), **`console-plugin-heroku`** (`heroku` — dyno state), and **`console-plugin-sentry`** (`sentry` — unresolved-issue count), each mapping to operational/degraded/down.

A **snapshot** aggregates the latest check per component into one overall health state (worst-wins; a not-yet-checked component never masks a real outage).

### Notifications

Console emits **events** on meaningful changes — a component going **down**, **degraded**, or **recovered**, and any **flag change** — and fans them out to **notifier plugins** listed in `CONSOLE_NOTIFY_PLUGINS` (you can run several at once). Three ship today: `console-plugin-slack` (Incoming Webhook, no bot token), `console-plugin-webhook` (POSTs each event as JSON, with an optional `X-Webhook-Secret`), and `console-plugin-email` (SMTP). Point `CONSOLE_NOTIFY_PLUGINS` at the sinks you want and you'll get alerts when a monitored service breaks or a flag is toggled.

## CLI

```text
console serve       Start the HTTP server (dashboard + API)
console flag        list | get | create | enable | disable | delete | eval
console status      list | add | check | snapshot | delete
console onboard     Onboard an app (Human or AI-Assisted mode)
console qr          Print a QR code to open the dashboard on your phone
console version
```

```bash
# Flags
console flag create checkout-v2 -desc "New checkout" -scope cohort -cohort power_users -rollout 100 -enabled
console flag eval   checkout-v2 -subject u-42 -attr cohort=power_users
console flag disable checkout-v2

# Status
console status add web -url https://example.com -name "Web app"
console status snapshot
```

## HTTP API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Aggregate health snapshot |
| `GET` | `/api/flags` | List flags |
| `POST` | `/api/flags` | Create a flag |
| `GET/PUT/DELETE` | `/api/flags/{key}` | Get / update / delete a flag |
| `POST` | `/api/flags/{key}/evaluate` | Evaluate for a subject (body: `{"key","attributes":{}}`) |
| `GET` | `/api/components` | List components |
| `POST` | `/api/components` | Create a component |
| `GET/PUT/DELETE` | `/api/components/{key}` | Get / update / delete a component |
| `POST` | `/api/components/{key}/check` | Run a check now |

```bash
curl -X POST localhost:8080/api/flags/new-dashboard/evaluate \
  -d '{"key":"user-123","attributes":{"audience":"beta"}}'
# → {"flag_key":"new-dashboard","enabled":true,"variant":"on","reason":"rollout_included"}
```

## Onboarding (Human + AI-Assisted)

```bash
# Human mode — interactive wizard
console onboard

# AI-Assisted mode — Claude drafts the plan (needs the anthropic LLM plugin)
export CONSOLE_LLM_PLUGIN=$PWD/bin/console-plugin-anthropic
export ANTHROPIC_API_KEY=sk-ant-...
console onboard -ai -name "Acme" -desc "A Rails store with a Sidekiq worker and a Postgres DB" \
  -guide acme-setup.md
```

Both modes produce a plan (components + flags), let you apply it, and can emit a Markdown setup guide. AI mode uses whichever LLM plugin `CONSOLE_LLM_PLUGIN` points at — `console-plugin-anthropic` (Claude), `console-plugin-openai` (GPT, default `gpt-4o-mini`), or `console-plugin-ollama` (local, no API key) — one provider at a time.

## Configuration

All configuration is via environment variables (CLI flags override per-command):

Core:

| Variable | Default | Purpose |
|---|---|---|
| `CONSOLE_ADDR` | `127.0.0.1:8080` | HTTP listen address (loopback) |
| `CONSOLE_DB` | `console.db` | SQLite path / DSN (`""` for in-memory) |

Plugin selection (each points at a `console-plugin-*` binary; unset = built-in default):

| Variable | Selects |
|---|---|
| `CONSOLE_STORE_PLUGIN` | storage backend (e.g. `console-plugin-postgres`); replaces built-in SQLite |
| `CONSOLE_STATUS_PLUGINS` | status providers (comma/space list: `console-plugin-cloudflare`, `-heroku`, `-sentry`); `http` is built-in |
| `CONSOLE_NOTIFY_PLUGINS` | notifier sinks (comma/space list: `console-plugin-slack`, `-webhook`, `-email`) |
| `CONSOLE_LLM_PLUGIN` | LLM for AI-Assisted onboarding (one of `console-plugin-anthropic`, `-openai`, `-ollama`); unset = AI mode off |

Read by plugins (inherited from the host environment; status providers also read per-component `config`):

| Variable | Used by |
|---|---|
| `CONSOLE_DB` (`postgres://` DSN) | `console-plugin-postgres` |
| `CLOUDFLARE_API_TOKEN` | `console-plugin-cloudflare` (default token) |
| `HEROKU_API_KEY` | `console-plugin-heroku` (default token) |
| `SENTRY_AUTH_TOKEN` | `console-plugin-sentry` (default token) |
| `CONSOLE_SLACK_WEBHOOK_URL` | `console-plugin-slack` |
| `CONSOLE_WEBHOOK_URL`, `CONSOLE_WEBHOOK_SECRET` | `console-plugin-webhook` (secret optional) |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `EMAIL_FROM`, `EMAIL_TO` | `console-plugin-email` |
| `ANTHROPIC_API_KEY`, `CONSOLE_MODEL` | `console-plugin-anthropic` |
| `OPENAI_API_KEY`, `CONSOLE_MODEL` | `console-plugin-openai` (default model `gpt-4o-mini`) |
| `OLLAMA_HOST`, `CONSOLE_MODEL` | `console-plugin-ollama` (no API key; default model `llama3.1`) |

### Plugins

Console is extended with **out-of-process plugins** — separate executables the host
launches and talks to over gRPC (the Terraform model), so you add a capability by
dropping a binary, with no core recompile. **All four seams** (storage, status,
notify, LLM) are plugins, and most have several to choose from; the core ships
with sensible built-in defaults (SQLite storage, the `http` status provider) so
it runs with zero plugins. Ten ship today: **store** — `postgres`; **status** —
`cloudflare`, `heroku`, `sentry`; **notify** — `slack`, `webhook`, `email`;
**llm** — `anthropic`, `openai`, `ollama`.

```bash
make build && make plugins                 # ./console + ./bin/console-plugin-*

# Postgres store backend:
export CONSOLE_STORE_PLUGIN=$PWD/bin/console-plugin-postgres
export CONSOLE_DB="postgres://user:pass@host:5432/console?sslmode=require"

# Status providers (comma/space list — Cloudflare, Heroku, Sentry):
export CONSOLE_STATUS_PLUGINS="$PWD/bin/console-plugin-cloudflare,$PWD/bin/console-plugin-heroku,$PWD/bin/console-plugin-sentry"
export CLOUDFLARE_API_TOKEN=... HEROKU_API_KEY=... SENTRY_AUTH_TOKEN=...

# Notifiers (comma/space list — Slack, webhook, email):
export CONSOLE_NOTIFY_PLUGINS="$PWD/bin/console-plugin-slack,$PWD/bin/console-plugin-webhook,$PWD/bin/console-plugin-email"
export CONSOLE_SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..."

# AI-Assisted onboarding (pick one LLM — Anthropic, OpenAI, or Ollama):
export CONSOLE_LLM_PLUGIN=$PWD/bin/console-plugin-anthropic
export ANTHROPIC_API_KEY=sk-ant-...

./console serve
```

See [docs/plugins-architecture.md](docs/plugins-architecture.md) for the full design,
and [docs/development.md](docs/development.md) for building and running on macOS, Linux, and Windows.

## Architecture

```
cmd/console/             CLI (serve, flag, status, onboard, qr, version)
cmd/console-plugin-*/    10 out-of-process plugin binaries (store/status/notify/llm)
internal/core/           domain types (Flag, Subject, Component, Health)
internal/config/         environment + flag configuration
internal/store/          Store interface + sqlite backend (pluggable)
internal/flags/          flag engine + deterministic evaluation
internal/status/         status engine + http provider (pluggable)
internal/notify/         Notifier interface + event fan-out (pluggable)
internal/llm/            LLM provider interface (anthropic/openai/ollama impls; pluggable)
internal/onboard/        Human + AI-Assisted onboarding
internal/plugin/         gRPC plugin host (launches console-plugin-* over gRPC)
internal/server/         HTTP API + server-rendered htmx dashboard
internal/web/            embedded dashboard assets/templates
internal/app/            composition root
docs/                    documentation site (GitHub Pages)
```

See [docs/architecture.md](docs/architecture.md) for the full design.

## Documentation

A full docs site lives in [`docs/`](docs/) (served via GitHub Pages from `docs/index.html`):

- [Getting started](docs/getting-started.md)
- [Feature flags](docs/flags.md)
- [Status monitoring](docs/status.md)
- [Notifications](docs/notifications.md)
- [Onboarding (Human + AI-Assisted)](docs/onboarding.md)
- [HTTP API reference](docs/api.md)
- [Architecture](docs/architecture.md)
- [Plugin architecture (out-of-process gRPC)](docs/plugins-architecture.md)
- [Writing plugins](docs/plugins-architecture.md#writing-a-plugin)
- [Developing Console (macOS / Linux / Windows)](docs/development.md)

## Development

```bash
make build   # build the binary
make test    # run all tests
make vet     # go vet
make fmt     # gofmt
```

For building, running with plugins, testing, and cross-compiling on macOS,
Linux, and Windows, see [docs/development.md](docs/development.md).

## Contributing

Console is built to be extended — new storage backends, status providers, and LLM providers all plug in behind interfaces. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Support

Questions or security reports: support@moosequest.net.

## License

Console is licensed under the **[GNU AGPL-3.0](LICENSE)** © MooseQuest LLC.

The AGPL keeps Console and its network-hosted derivatives open: if you modify
Console and run it as a service, you must offer your users the modified source.
Contributions are accepted under a [Contributor License Agreement](CLA.md) so the
project can be sustainably maintained and dual-licensed. If the AGPL doesn't fit
your use case, a commercial license may be available — open an issue to ask.
