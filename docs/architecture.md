# Architecture

Console is a single static Go binary that bundles a feature-flag engine, a
status-monitoring engine, a JSON API, a server-rendered dashboard, a CLI, and
onboarding — all over one embedded SQLite database. This document explains how
the pieces fit together and why.

- [Design goals](#design-goals)
- [Package layout](#package-layout)
- [The interface seams](#the-interface-seams)
- [Data flow](#data-flow)
- [Single binary + SQLite rationale](#single-binary--sqlite-rationale)

## Design goals

- **One binary, no dependencies.** Pure-Go SQLite (no cgo) yields a truly static
  binary you can drop on any host. `./console serve` is the whole install.
- **Modular by design.** Storage, status providers, and LLM providers each sit
  behind a small Go interface, so they can be swapped or extended without
  touching the core.
- **API-first.** The dashboard is a client of the same HTTP API your apps use.
- **Deterministic and stdlib-first.** Flag evaluation is reproducible; the
  codebase prefers the standard library and minimal dependencies.

## Package layout

```text
cmd/console/        CLI entrypoint (serve, flag, status, onboard, version)
internal/core/      domain types — the shared vocabulary (Flag, Subject,
                    Evaluation, Component, Check, Health) + sentinel errors.
                    Depends on nothing else, so it never causes import cycles.
internal/config/    runtime configuration (env vars + defaults, no YAML parser)
internal/store/     Store interface (the persistence seam)
internal/store/sqlite/   the default SQLite backend (modernc.org/sqlite, no cgo)
internal/flags/     flag engine — deterministic evaluation over a FlagStore
internal/status/    status engine + the built-in http Provider
internal/llm/        LLM Provider interface + Anthropic implementation
internal/onboard/   Human + AI-Assisted onboarding → a Plan (Apply / Guide)
internal/server/    HTTP API + server-rendered htmx dashboard
internal/web/        embedded templates + static assets (CSS, vendored JS)
internal/app/        composition root — wires everything into one App
docs/               this documentation site (served via GitHub Pages)
```

The dependency direction is one-way: `core` is imported by everyone and imports
no other package in the project; the engines depend on the store *interfaces*
(not the concrete backend); the server and CLI depend on the assembled `App`.

## The interface seams

Three small interfaces are the extension points. Each has a default
implementation; adding another means implementing the interface and wiring it in
`internal/app/app.go`. See [plugins](plugins.md) for step-by-step instructions.

### `store.Store` — persistence

```go
type Store interface {
    FlagStore        // CreateFlag / GetFlag / ListFlags / UpdateFlag / DeleteFlag
    ComponentStore   // CreateComponent / GetComponent / ListComponents / UpdateComponent / DeleteComponent
    CheckStore       // RecordCheck / LatestCheck / LatestChecks
    Ping(ctx context.Context) error
    Close() error
}
```

Implementations must be safe for concurrent use; lookups return
`core.ErrNotFound` when a key is absent and creates return `core.ErrConflict`
when it already exists. The default backend is SQLite; nothing above this seam
knows which backend is in use, so a Postgres backend is a drop-in.

### `status.Provider` — health checks

```go
type Provider interface {
    Name() string                                       // e.g. "http"
    Check(ctx context.Context, comp core.Component) core.Check
}
```

The status engine registers providers by name and dispatches per component via
`core.Component.Provider`. The built-in provider is `http`.

### `llm.Provider` — AI-Assisted onboarding

```go
type Provider interface {
    Name() string                                       // e.g. "anthropic"
    Complete(ctx context.Context, req Request) (string, error)
}
```

Intentionally minimal — a single text completion — so new providers (OpenAI,
local models) are cheap to add. The default is Anthropic (Claude); when no
provider is configured the field is nil and AI-Assisted onboarding is disabled
(Human mode still works).

## Data flow

The composition root (`internal/app`) opens the store and constructs the
engines, registering the built-in HTTP provider and selecting the LLM provider:

```text
config.FromEnv ──▶ app.New
                     ├─ sqlite.Open(cfg.DB)            ─▶ store.Store
                     ├─ flags.New(store)               ─▶ *flags.Engine
                     ├─ status.New(store, store, &HTTPProvider{}) ─▶ *status.Engine
                     └─ newLLM(cfg)                    ─▶ llm.Provider (or nil)
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
- **Embedded by default, pluggable later.** SQLite is perfect for a single node
  and zero-ops local use; because persistence is behind `store.Store`, moving to
  Postgres is a backend swap, not a rewrite. An in-memory database
  (`CONSOLE_DB=""`) makes tests and experiments instant.
- **Embedded assets.** Templates and static files are embedded via `embed`, so
  the binary serves the full dashboard with no external files to ship.

The result is an artifact you can `scp` to a box and run — the control plane is
the binary.
