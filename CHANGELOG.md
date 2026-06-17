# Changelog

All notable changes to Console are documented here. This project adheres to
[Semantic Versioning](https://semver.org). While on `0.x`, minor releases may
include breaking changes to the API and plugin protocol.

## [0.2.1] - 2026-06-17

Security hardening release (from a repo-wide security review + `govulncheck`).

### Security
- Bumped `golang.org/x/net` → v0.55.0 and `golang.org/x/sys` → v0.45.0, clearing
  8 transitive advisories (none were reachable from Console's code).
- Plugins: enabled go-plugin **AutoMTLS** so only the launching host can talk to
  a plugin's gRPC port.
- The HTTP server now **defaults to loopback** (`127.0.0.1:8080`) — it has no
  built-in auth yet, so exposing it is now a deliberate opt-in behind an
  authenticating reverse proxy. Added server timeouts, a 1 MiB request-body cap,
  and security headers (nosniff, `X-Frame-Options`, `Referrer-Policy`, CSP).
- Bounded LLM provider response reads; bounded `RunAll` concurrency; redacted
  secret-bearing URLs from notifier error logs.
- Supply chain: pinned GitHub Actions to commit SHAs, added a `govulncheck` CI
  job, and enabled Dependabot.
- Added security SOPs under `docs/security/` (supply chain, plugin trust,
  runtime hardening) and a hardening/deployment note in `SECURITY.md`.

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

[0.2.1]: https://github.com/MooseQuest/console/releases/tag/v0.2.1
[0.2.0]: https://github.com/MooseQuest/console/releases/tag/v0.2.0
[0.1.0]: https://github.com/MooseQuest/console/releases/tag/v0.1.0
