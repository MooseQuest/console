# Contributing to Console

Thanks for your interest in Console (v0.3.0). It's built to be extended — new
storage backends, status providers, notifiers, and LLM providers all plug in as
out-of-process `console-plugin-*` binaries. This guide covers how to build, test,
and land changes.

- [License and the CLA](#license-and-the-cla)
- [Build and test](#build-and-test)
- [Repository layout](#repository-layout)
- [Coding conventions](#coding-conventions)
- [Adding a plugin](#adding-a-plugin)
- [Pull requests and commits](#pull-requests-and-commits)

## License and the CLA

Console is licensed under the **[GNU AGPL-3.0](LICENSE)**. By contributing, you
agree your contribution is provided under that license, and you sign the
project's **[Contributor License Agreement](CLA.md)** — a one-time agreement that
lets MooseQuest LLC maintain and, where needed, dual-license the project (an
AGPL community edition plus a possible commercial edition). The CLA does not take
away any of your own rights to your work.

Until a CLA bot is set up, add this line to your first pull request and add your
name to `CONTRIBUTORS.md` in the same PR:

```
I have read the CLA document and I hereby sign the CLA.
```

> The CLA is currently a **draft pending attorney review** — see [CLA.md](CLA.md).

## Build and test

Console needs **Go 1.25+** and no cgo toolchain — the embedded database is the
pure-Go `modernc.org/sqlite`, so a plain `go build` produces a static binary.

```bash
make build   # build the binary (or: go build -o console ./cmd/console)
make test    # run all tests
make vet     # go vet ./...
make fmt     # gofmt the tree
```

Run `make fmt`, `make vet`, and `make test` before opening a pull request (PR). An in-memory
database makes tests instant — pass `CONSOLE_DB=""` (or `:memory:`) when running
the binary against a throwaway store.

```bash
CONSOLE_DB="" ./console serve   # nothing persisted between runs
```

For step-by-step build, test, run-with-plugins, and cross-compile instructions
on **macOS, Linux, and Windows** (including the `go`-only commands when `make`
isn't available), see [docs/development.md](docs/development.md).

## Repository layout

```text
cmd/console/             CLI entrypoint (serve, flag, status, onboard, qr, version)
cmd/console-plugin-*/    10 out-of-process plugin binaries (store/status/notify/llm)
internal/core/           domain types (Flag, Subject, Evaluation, Component, Check,
                         Health) + sentinel errors; depends on nothing else
internal/config/         runtime configuration (env vars + defaults)
internal/store/          Store interface (persistence seam) + default SQLite backend
internal/flags/          flag engine + deterministic evaluation
internal/status/         status engine + built-in http provider
internal/notify/         Notifier interface + event fan-out
internal/llm/            LLM provider interface (anthropic/openai/ollama impls)
internal/onboard/        Human + AI-Assisted onboarding
internal/plugin/         gRPC plugin host (launches console-plugin-* over gRPC)
internal/server/         HTTP API + server-rendered htmx dashboard
internal/web/            embedded templates + static assets
internal/app/            composition root (wires everything into one App)
docs/                    documentation site + deep-dive references
```

See [docs/architecture.md](docs/architecture.md) for the full design and the
dependency direction between packages.

## Coding conventions

- **Doc comments.** Every exported type, function, and package has a comment that
  begins with its name (`// Engine evaluates feature flags…`). Match the tone of
  the existing comments — they explain *why*, not just *what*.
- **Stdlib-first, minimal dependencies.** Reach for the standard library before a
  third-party package. New dependencies need a clear justification in the PR.
- **Keep the binary cgo-free.** Prefer pure-Go drivers and libraries so the
  binary stays statically linkable and trivially cross-compilable. (This is why
  storage uses `modernc.org/sqlite`.)
- **Thin adapters, fat engines.** Business logic lives in the engines
  (`internal/flags`, `internal/status`); the server and CLI only decode, call an
  engine, and encode. Keep it that way.
- **Determinism.** Flag evaluation must stay deterministic per `(flag, subject)`
  — never introduce randomness or wall-clock dependence into a decision.
- **Errors.** Use the `core` sentinels (`core.ErrNotFound`, `core.ErrConflict`)
  so the API layer maps them to the right HTTP status; wrap with `%w`.
- **Formatting.** `gofmt` (via `make fmt`) is non-negotiable.

## Adding a plugin

Console has **four** extension seams — `store.Store`, `status.Provider`,
`notify.Notifier`, and `llm.Provider`. Plugins are **out-of-process**: each is its
own `cmd/console-plugin-<name>` binary that the host launches and talks to over
gRPC, selected at runtime by the matching env var (`CONSOLE_STORE_PLUGIN`,
`CONSOLE_STATUS_PLUGINS`, `CONSOLE_NOTIFY_PLUGINS`, `CONSOLE_LLM_PLUGIN`). Nothing
is hand-wired into `internal/app/app.go`; build the binaries with `make plugins`.

To add one: implement the seam interface, build a `cmd/console-plugin-<name>` that
serves it, and point the host at the binary via its env var. The step-by-step
template and the full design are in
[docs/plugins-architecture.md](docs/plugins-architecture.md).

Honor the interface contracts — for stores, that means concurrency-safety and
returning `core.ErrNotFound` / `core.ErrConflict` — and add table-tests with a
fake, matching the existing engine and provider tests.

## Pull requests and commits

- **Tests for new behavior.** Any new behavior or bug fix should come with tests.
  The engines are table-tested against fakes; follow that pattern.
- **Green before review.** `make fmt && make vet && make test` should pass.
- **Clear commit messages.** Write an imperative subject line ("Add TCP status
  provider"), and explain *why* in the body when it isn't obvious. Keep unrelated
  changes in separate commits.
- **Scoped PRs.** One logical change per PR. Update the relevant docs in `docs/`
  (and the README, if you change a documented fact) in the same PR.
- **Describe the change.** In the PR description, say what changed, why, and how
  you tested it.

Interfaces may still change before v1 — if your change touches a public seam,
call that out so we can coordinate.
