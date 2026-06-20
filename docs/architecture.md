# Architecture

Console is a single static Go binary that bundles a feature-flag engine, a
status-monitoring engine, a JSON API, a server-rendered dashboard, a CLI, and
onboarding — over an embedded SQLite database by default, with pluggable storage
and providers when you want them. This document explains how the pieces fit
together and why.

- [Design goals](#design-goals)
- [Package layout](#package-layout)
- [The interface seams](#the-interface-seams)
- [Data flow](#data-flow)
- [Single binary + SQLite rationale](#single-binary--sqlite-rationale)

## Design goals

- **One binary, minimal dependencies.** Pure-Go SQLite (no cgo) yields a truly
  static binary you can drop on any host. `./console serve` is the whole install,
  and it runs with zero plugins.
- **Modular by design.** Storage, status providers, notifiers, and LLM providers
  each sit behind a small Go interface, served by out-of-process plugins, so they
  can be swapped or extended without touching the core.
- **API-first.** The dashboard is a client of the same HTTP API your apps use.
- **Deterministic and stdlib-first.** Flag evaluation is reproducible; the
  codebase prefers the standard library and minimal dependencies.

## Package layout

```text
cmd/console/        CLI entrypoint (serve, flag, status, onboard, qr, version)
cmd/console-plugin-*/    the 10 out-of-process plugin executables (store,
                    status, notify, and llm providers — see plugin catalog)
internal/core/      domain types — the shared vocabulary (Flag, Subject,
                    Evaluation, Component, Check, Health) + sentinel errors.
                    Depends on nothing else, so it never causes import cycles.
internal/config/    runtime configuration (env vars + defaults, no YAML parser)
internal/store/     Store interface (the persistence seam)
internal/store/sqlite/   the default SQLite backend (modernc.org/sqlite, no cgo)
internal/store/postgres/ the Postgres backend (served as a store plugin)
internal/flags/     flag engine — deterministic evaluation over a FlagStore
internal/status/    status engine + the built-in http Provider
internal/status/{cloudflare,heroku,sentry}/   plugin status providers
internal/notify/    Notifier interface + Dispatcher (the notify seam)
internal/notify/{slack,webhook,email}/   plugin notifier sinks
internal/llm/       LLM Provider interface (provider.go)
internal/llm/{anthropic,openai,ollama}/   plugin LLM implementations
internal/plugin/    out-of-process plugin host: loaders, Serve helpers, and the
                    gRPC client/server adapters (+ internal/plugin/proto)
internal/onboard/   Human + AI-Assisted onboarding → a Plan (Apply / Guide)
internal/server/    HTTP API + server-rendered htmx dashboard
internal/web/       embedded templates + static assets (CSS, vendored JS)
internal/app/       composition root — wires everything into one App
proto/              the per-seam gRPC contracts (store/status/notify/llm)
docs/               this documentation site (served via GitHub Pages)
```

The dependency direction is one-way: `core` is imported by everyone and imports
no other package in the project; the engines depend on the store *interfaces*
(not the concrete backend); the server and CLI depend on the assembled `App`.

## The interface seams

Four small interfaces are the extension points. Each has a default that is built
into the core, and each can be **extended out-of-process**: a plugin is a
separate `console-plugin-*` executable the host launches and talks to over gRPC.
Nothing is hand-wired in `internal/app/app.go` — `loadPlugins` loads whatever the
`CONSOLE_*_PLUGIN(S)` variables point at. See
[plugin architecture](plugins-architecture.md) for the canonical mechanics and
[plugins](plugins.md) for an authoring guide.

| Seam | Interface | Loader | Selected by | Default |
|---|---|---|---|---|
| Storage | `store.Store` | `plugin.LoadStore` | `CONSOLE_STORE_PLUGIN` (one) | built-in SQLite |
| Status | `status.Provider` | `plugin.LoadStatusProvider` | `CONSOLE_STATUS_PLUGINS` (list) | built-in `http` |
| Notify | `notify.Notifier` | `plugin.LoadNotifier` | `CONSOLE_NOTIFY_PLUGINS` (list) | none (no sink) |
| LLM | `llm.Provider` | `plugin.LoadLLM` | `CONSOLE_LLM_PLUGIN` (one) | none (Human mode) |

- **`store.Store`** persists every domain object (flags, components, checks).
  Implementations must be safe for concurrent use; lookups return
  `core.ErrNotFound` and creates return `core.ErrConflict`. Nothing above this
  seam knows which backend is in use, so the Postgres plugin is a drop-in for the
  built-in SQLite backend.
- **`status.Provider`** runs one health check (`Check` → `core.Check`). The
  status engine registers providers by name and dispatches per component via
  `core.Component.Provider`; the built-in provider is `http`.
- **`notify.Notifier`** delivers a `core.Event` (a status transition or flag
  change) to an external destination. A `notify.Dispatcher` fans each emitted
  event out to every registered sink; the engines only emit when at least one
  sink is loaded.
- **`llm.Provider`** is a single text completion used by AI-Assisted onboarding.
  Intentionally minimal, so new providers are cheap to add. With none configured
  the field is nil and AI-Assisted onboarding is unavailable (Human mode still
  works). The interface lives in `internal/llm/provider.go`; the Anthropic,
  OpenAI, and Ollama implementations live in `internal/llm/{anthropic,openai,ollama}/`.

The interface definitions live in their packages (`internal/store`,
`internal/status`, `internal/notify`, `internal/llm`); see the source or
[plugins](plugins.md) for the exact method sets.

## Data flow

The composition root (`internal/app`) opens the store, constructs the engines
(registering the built-in HTTP provider), then `loadPlugins` launches any
configured out-of-process seam plugins. `openStore` branches on
`cfg.StorePlugin`; the LLM provider, status-provider, and notifier plugins are
each loaded via their `plugin.Load*` helper:

```text
config.FromEnv ──▶ app.New
                     ├─ openStore(cfg)                 ─▶ store.Store
                     │    cfg.StorePlugin set? plugin.LoadStore : sqlite.Open
                     ├─ flags.New(store)               ─▶ *flags.Engine
                     ├─ status.New(store, store, &HTTPProvider{}) ─▶ *status.Engine
                     └─ loadPlugins(cfg)
                          ├─ plugin.LoadNotifier(path) ─▶ Notify.Register (list)
                          ├─ plugin.LoadStatusProvider(path) ─▶ Status.Register (list)
                          └─ plugin.LoadLLM(cfg.LLMPlugin) ─▶ llm.Provider (or nil)
```

**Flag evaluation** (`POST /api/flags/{key}/evaluate` or `console flag eval`):

```text
request ─▶ server/CLI ─▶ flags.Engine.Evaluate
            load flag (store) ─▶ enabled gate ─▶ scope gate
            ─▶ rollout gate (FNV hash of "flag:subject")
            ─▶ variant selection ─▶ core.Evaluation
```

**Status check** (`POST /api/components/{key}/check` or `console status check`):

```text
request ─▶ server/CLI ─▶ status.Engine.Run(component)
            select Provider by name ─▶ Provider.Check ─▶ core.Check
            ─▶ store.RecordCheck
Snapshot:  store.LatestChecks ─▶ aggregate (worst-wins, unknown least-severe) ─▶ core.Health
```

The HTTP server (`internal/server`) is deliberately thin: every handler decodes
a request, calls an engine, and encodes the result. The dashboard is rendered
from embedded `html/template` files with htmx for live toggles and re-checks —
it carries no business logic of its own.

## Single binary + SQLite rationale

- **No cgo.** Using `modernc.org/sqlite` (a pure-Go SQLite) means the binary
  cross-compiles trivially and has no native build toolchain requirement. Keeping
  the codebase cgo-free is a deliberate constraint (see
  [CONTRIBUTING](../CONTRIBUTING.md)).
- **Embedded by default, pluggable on demand.** SQLite is perfect for a single
  node and zero-ops local use; because persistence is behind `store.Store`,
  moving to Postgres is just pointing `CONSOLE_STORE_PLUGIN` at the Postgres
  plugin — a config change, not a rewrite. An in-memory database
  (`CONSOLE_DB=""`) makes tests and experiments instant.
- **Embedded assets.** Templates and static files are embedded via `embed`, so
  the binary serves the full dashboard with no external files to ship.

The result is an artifact you can `scp` to a box and run — the control plane is
the binary.
