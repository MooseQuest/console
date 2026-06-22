# Changelog

All notable changes to Console are documented here. This project adheres to
[Semantic Versioning](https://semver.org). While on `0.x`, minor releases may
include breaking changes to the API and plugin protocol.

## [0.4.0] - 2026-06-22

### Plugins
- **Notifiers** — `console-plugin-discord` (posts each event to a Discord
  channel Webhook as a colored embed; no bot token, reads
  `CONSOLE_DISCORD_WEBHOOK_URL`) and `console-plugin-pagerduty` (PagerDuty Events
  API v2 — a component going down/degraded *triggers* an alert and the matching
  recovery *resolves* it, correlated by a per-component dedup key so the pair
  becomes one incident; flag changes are skipped; reads
  `CONSOLE_PAGERDUTY_ROUTING_KEY`). The notifier seam now ships five sinks.

## [0.3.1] - 2026-06-20

Cleanup + hardening release.

### Changed
- The binary now reports an accurate version: release builds stamp the tag, and
  `go install`-ed builds fall back to the module version (was a hardcoded
  `0.1.0-dev`).
- Removed dead configuration fields (`CONSOLE_LLM_PROVIDER` and the unused
  Anthropic/Cloudflare/model/Slack config) — AI-Assisted onboarding is enabled
  solely via `CONSOLE_LLM_PLUGIN`; provider-specific env vars are read by the
  relevant plugin. CLI help and the generated onboarding guide updated to match.
- Set the `go.mod` floor to **Go 1.25.11** — the patched toolchain that clears
  the latest stdlib advisories (was over-specified at 1.26.4); docs updated.

### Security
- Added a `Permissions-Policy` response header (`camera=(), microphone=(),
  geolocation=()`).
- Pinned GitHub Actions bumped to `actions/checkout@v6` and `actions/setup-go@v6`.

### Docs
- Implemented the librarian documentation-accuracy pass and refreshed the docs
  site (`docs/index.html`) to the out-of-process four-seam plugin model.

## [0.3.0] - 2026-06-17

### Added
- **Open the dashboard on your phone** — `console qr` renders a scannable
  terminal QR code of `http://<LAN-IP>:<port>`, and `console serve --qr` prints
  it at startup. `-url` encodes any address (e.g. a tunnel URL) for remote
  access. Same-Wi-Fi use is the safe default; it warns when bound to loopback
  and that exposing the (still unauthenticated) dashboard needs a trusted
  network. README documents the LAN and tunnel paths.

### Distribution
- Release bundles now include **Windows** (amd64/arm64) alongside macOS/Linux,
  plus a **`SHA256SUMS.txt`** for integrity. New `make dist` builds all bundles.
- README **Install** section with verify-and-run steps per OS.

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

[0.4.0]: https://github.com/MooseQuest/console/releases/tag/v0.4.0
[0.3.1]: https://github.com/MooseQuest/console/releases/tag/v0.3.1
[0.3.0]: https://github.com/MooseQuest/console/releases/tag/v0.3.0
[0.2.1]: https://github.com/MooseQuest/console/releases/tag/v0.2.1
[0.2.0]: https://github.com/MooseQuest/console/releases/tag/v0.2.0
[0.1.0]: https://github.com/MooseQuest/console/releases/tag/v0.1.0
