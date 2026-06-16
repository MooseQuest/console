<div align="center">

# Console

**A modular, self-hostable control plane for the apps you build — feature flags + status monitoring in one small binary.**

[Quickstart](#quickstart) · [Concepts](#concepts) · [CLI](#cli) · [HTTP API](#http-api) · [Onboarding](#onboarding-human--ai-assisted) · [Docs](./docs)

</div>

---

Console is a one-stop control plane for your application: ship features safely behind **feature flags**, and watch your services with **status checks** — from a single dashboard, a JSON API, and a CLI. It runs as **one static Go binary** with an embedded SQLite database, so getting started is `./console serve`.

It ships with two ways to get an app set up:

- **Human mode** — an interactive wizard walks you through what to monitor and which flags to create.
- **AI-Assisted mode** — describe your app in a sentence and an LLM (Claude by default) drafts the plan for you.

> Status: **early / v0.1**. The core engine, API, dashboard, CLI, and onboarding are working and tested. Interfaces may still change before v1.

## Why Console

- **One binary, no dependencies.** Pure-Go SQLite (no cgo) means a truly static binary you can drop on any host. Point it at Postgres later when you outgrow a single node.
- **Modular by design.** Storage, status providers, and LLM providers all sit behind small Go interfaces — swap or extend them without touching the core.
- **API-first.** The dashboard is just a client of the same HTTP API your apps and SDKs use.
- **Deterministic flag evaluation.** The same `(flag, subject)` always resolves the same way, with a stable percentage rollout you can reason about.

## Screenshots

| Overview | Flags | Status |
|---|---|---|
| ![Overview](docs/assets/overview.png) | ![Flags](docs/assets/flags.png) | ![Status](docs/assets/status.png) |

## Quickstart

```bash
# 1. Build (needs Go 1.22+)
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

Providers are pluggable. Beyond the built-in `http` provider, Console ships a **`cloudflare-workers`** provider that reads a Worker's recent invocation analytics (Cloudflare GraphQL API) and maps its error rate to operational/degraded/down — config keys `account_id`, `worker`, optional `api_token` (falls back to `CLOUDFLARE_API_TOKEN`), `window`, `degraded_pct`, `down_pct`.

A **snapshot** aggregates the latest check per component into one overall health state (worst-wins; a not-yet-checked component never masks a real outage).

### Notifications

Console emits **events** on meaningful changes — a component going **down**, **degraded**, or **recovered**, and any **flag change** — and fans them out to **notifier plugins**. Slack ships as the `console-plugin-slack` plugin (posts to an Incoming Webhook, no bot token): point `CONSOLE_NOTIFY_PLUGINS` at it and set `CONSOLE_SLACK_WEBHOOK_URL`, and you'll get alerts when a monitored service breaks or a flag is toggled. A webhook or email sink is just another `notify.Notifier` served as a plugin.

## CLI

```text
console serve       Start the HTTP server (dashboard + API)
console flag        list | get | create | enable | disable | delete | eval
console status      list | add | check | snapshot | delete
console onboard     Onboard an app (Human or AI-Assisted mode)
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

# AI-Assisted mode — Claude drafts the plan (needs ANTHROPIC_API_KEY)
export ANTHROPIC_API_KEY=sk-ant-...
console onboard -ai -name "Acme" -desc "A Rails store with a Sidekiq worker and a Postgres DB" \
  -guide acme-setup.md
```

Both modes produce a plan (components + flags), let you apply it, and can emit a Markdown setup guide.

## Configuration

All configuration is via environment variables (CLI flags override per-command):

| Variable | Default | Purpose |
|---|---|---|
| `CONSOLE_ADDR` | `:8080` | HTTP listen address |
| `CONSOLE_DB` | `console.db` | SQLite path / DSN (`""` for in-memory) |
| `CONSOLE_LLM_PROVIDER` | `anthropic` | LLM provider for AI mode (`""` to disable) |
| `CONSOLE_MODEL` | provider default | LLM model override |
| `ANTHROPIC_API_KEY` | — | API key for the Anthropic provider |
| `CLOUDFLARE_API_TOKEN` | — | Default token for the Cloudflare Workers status provider |
| `CONSOLE_STORE_PLUGIN` | — | Path to an out-of-process storage plugin (e.g. `console-plugin-postgres`); replaces built-in SQLite |
| `CONSOLE_NOTIFY_PLUGINS` | — | Comma/space-separated paths to notifier plugins (e.g. `console-plugin-slack`) |
| `CONSOLE_SLACK_WEBHOOK_URL` | — | Slack Incoming Webhook URL, read by the `console-plugin-slack` plugin |

### Plugins

Console is extended with **out-of-process plugins** — separate executables the host
launches and talks to over gRPC (the Terraform model), so you add a capability by
dropping a binary, with no core recompile. SQLite is built in as the default; other
backends ship as plugins. For example, to use Postgres:

```bash
make build && make plugins                 # ./console + ./bin/console-plugin-*

# Postgres store backend:
export CONSOLE_STORE_PLUGIN=$PWD/bin/console-plugin-postgres
export CONSOLE_DB="postgres://user:pass@host:5432/console?sslmode=require"

# Slack notifications:
export CONSOLE_NOTIFY_PLUGINS=$PWD/bin/console-plugin-slack
export CONSOLE_SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..."

./console serve
```

See [docs/plugins-architecture.md](docs/plugins-architecture.md) for the full design.

## Architecture

```
cmd/console/        CLI (serve, flag, status, onboard)
internal/core/      domain types (Flag, Subject, Component, Health)
internal/store/     Store interface + sqlite backend (pluggable)
internal/flags/     flag engine + deterministic evaluation
internal/status/    status engine + http provider (pluggable)
internal/llm/        LLM provider interface + Anthropic (pluggable)
internal/onboard/   Human + AI-Assisted onboarding
internal/server/    HTTP API + server-rendered htmx dashboard
internal/app/       composition root
docs/               documentation site (GitHub Pages)
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
- [Writing plugins](docs/plugins.md)

## Development

```bash
make build   # build the binary
make test    # run all tests
make vet     # go vet
make fmt     # gofmt
```

## Contributing

Console is built to be extended — new storage backends, status providers, and LLM providers all plug in behind interfaces. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Console is licensed under the **[GNU AGPL-3.0](LICENSE)** © MooseQuest LLC.

The AGPL keeps Console and its network-hosted derivatives open: if you modify
Console and run it as a service, you must offer your users the modified source.
Contributions are accepted under a [Contributor License Agreement](CLA.md) so the
project can be sustainably maintained and dual-licensed. If the AGPL doesn't fit
your use case, a commercial license may be available — open an issue to ask.
