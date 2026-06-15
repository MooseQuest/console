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
another process.

## Seams

| Seam | Interface | Status |
|---|---|---|
| Storage | `store.Store` | **out-of-process** (this release) — e.g. `console-plugin-postgres` |
| Status | `status.Provider` | converting to the same model |
| Notify | `notify.Notifier` | converting to the same model |
| LLM | `llm.Provider` | converting to the same model |

SQLite stays **built into the core** as the zero-dependency default; everything
else is a plugin.

## Using the Postgres store plugin

```bash
# 1. Build the host and the plugin
make build && make plugins        # -> ./console and ./bin/console-plugin-postgres

# 2. Point the host at the plugin + your Postgres DSN
export CONSOLE_STORE_PLUGIN=$PWD/bin/console-plugin-postgres
export CONSOLE_DB="postgres://user:pass@host:5432/console?sslmode=require"

./console serve
```

The host launches the plugin, performs the handshake, and uses it as the store
over gRPC. The plugin inherits the host's environment, so it reads `CONSOLE_DB`
itself. Without `CONSOLE_STORE_PLUGIN`, the host uses built-in SQLite.

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
