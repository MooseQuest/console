# Changelog

All notable changes to Console are documented here. This project adheres to
[Semantic Versioning](https://semver.org). While on `0.x`, minor releases may
include breaking changes to the API and plugin protocol.

## [0.2.0] - 2026-06-16

Six new plugins, a cross-platform developer guide, and a support contact.

### Plugins

- **Status providers** — `console-plugin-heroku` (`heroku`; maps dyno state) and
  `console-plugin-sentry` (`sentry`; maps a project's unresolved-issue count),
  both selected via `CONSOLE_STATUS_PLUGINS`.
- **Notifiers** — `console-plugin-webhook` (POSTs each event as JSON, with an
  optional `X-Webhook-Secret`) and `console-plugin-email` (SMTP), both selected
  via `CONSOLE_NOTIFY_PLUGINS`.
- **LLM providers** — `console-plugin-openai` (GPT; default model `gpt-4o-mini`)
  and `console-plugin-ollama` (local, no API key; default model `llama3.1`),
  selected via `CONSOLE_LLM_PLUGIN`.

The plugin catalog now spans ten binaries across all four seams; see
[docs/plugins-architecture.md](docs/plugins-architecture.md).

### Project

- **Developer guide** — `docs/development.md` covers building, running with
  plugins, testing, and cross-compiling on **macOS, Linux, and Windows**.
- **Support contact** — questions and security reports go to
  **support@moosequest.net** (see `SECURITY.md`).

## [0.1.0] - 2026-06-16

First release: a modular, self-hostable control plane for feature flags and
status monitoring, with an out-of-process plugin system.

### Core

- **Feature flags** — scopes (`all` / `beta` / `alpha` / `cohort` / `experiment`),
  deterministic percentage rollout (stable per `flag`+`subject`), and boolean or
  weighted multivariate variants.
- **Status monitoring** — components checked by named providers, with a built-in
  `http` provider and worst-wins health aggregation (an unchecked component never
  masks a real outage).
- **Events & notifications** — the engines emit events on health transitions
  (down / degraded / recovered) and flag changes, fanned out to notifier sinks.
- **Onboarding** — a Human (interactive) mode and an AI-Assisted mode that drafts
  a setup plan, both producing an applyable plan and a Markdown setup guide.
- **Interfaces** — JSON API, a server-rendered htmx dashboard, and a `console`
  CLI (`serve`, `flag`, `status`, `onboard`).
- **Single static binary** — pure-Go, cgo-free, with an embedded SQLite default.

### Plugins (out-of-process, gRPC)

All four extension seams run as separate executables the host launches and talks
to over gRPC ([hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)), so
capabilities are added by dropping a binary — no core recompile. Defaults
(SQLite storage, `http` status) stay built in.

- `console-plugin-postgres` — Postgres storage backend (`CONSOLE_STORE_PLUGIN`).
- `console-plugin-cloudflare` — Cloudflare Workers health status provider
  (`CONSOLE_STATUS_PLUGINS`).
- `console-plugin-slack` — Slack Incoming Webhook notifier (`CONSOLE_NOTIFY_PLUGINS`).
- `console-plugin-anthropic` — Anthropic (Claude) LLM for AI-Assisted onboarding
  (`CONSOLE_LLM_PLUGIN`).

Host↔plugin compatibility is governed by the go-plugin handshake
`ProtocolVersion` (currently `1`), independent of this release version.

### Project

- Licensed under **AGPL-3.0** with a contributor CLA.
- Documentation site under `docs/` (GitHub Pages).

[0.2.0]: https://github.com/MooseQuest/console/releases/tag/v0.2.0
[0.1.0]: https://github.com/MooseQuest/console/releases/tag/v0.1.0
