# Developing Console

This guide covers building, running, and testing Console on **macOS, Linux, and
Windows**. Console is pure Go with no cgo, so it builds and cross-compiles the
same way everywhere — including the plugins.

- [Prerequisites](#prerequisites)
- [macOS / Linux](#macos--linux)
- [Windows](#windows)
- [Running with plugins](#running-with-plugins)
- [Tests](#tests)
- [Regenerating gRPC stubs](#regenerating-grpc-stubs)
- [Cross-compiling](#cross-compiling)

## Prerequisites

- **Go 1.25+** — <https://go.dev/dl/>. Verify with `go version`.
- **git**.
- (Optional) **GNU Make** — convenient on macOS/Linux; not required on Windows.
- (Only if you change `proto/`) **protoc** plus `protoc-gen-go` and
  `protoc-gen-go-grpc` — see [Regenerating gRPC stubs](#regenerating-grpc-stubs).
  The generated `*.pb.go` are committed, so a normal build needs none of these.

## macOS / Linux

```bash
git clone https://github.com/MooseQuest/console.git
cd console

make build      # -> ./console
make plugins    # -> ./bin/console-plugin-*  (all plugin binaries)
make test       # run all tests
make vet        # go vet
make fmt        # gofmt

./console serve # http://localhost:8080
```

Without Make, the underlying commands are just:

```bash
go build -o console ./cmd/console
go build -o bin/ ./cmd/console-plugin-...
go test ./...
```

## Windows

Make isn't installed by default on Windows, so use `go` directly (PowerShell
shown). Go produces `.exe` binaries.

```powershell
git clone https://github.com/MooseQuest/console.git
cd console

# Build the host
go build -o console.exe ./cmd/console

# Build every plugin into .\bin (each becomes console-plugin-*.exe)
go build -o bin\ ./cmd/console-plugin-...

# Test / vet / format
go test ./...
go vet ./...
gofmt -w cmd internal

.\console.exe serve   # http://localhost:8080
```

If you prefer `make` on Windows, install it via [Scoop](https://scoop.sh)
(`scoop install make`), [Chocolatey](https://chocolatey.org) (`choco install make`),
or use Git Bash / WSL — then the macOS/Linux `make` targets work unchanged.

> The plugin system uses gRPC over a local TCP port (via hashicorp/go-plugin),
> which works the same on Windows as on Unix — no special setup.

## Running with plugins

Plugins are separate executables the host launches; you point the host at them
with environment variables. On Windows the paths end in `.exe`.

**macOS / Linux (bash):**

```bash
export CONSOLE_STORE_PLUGIN=$PWD/bin/console-plugin-postgres
export CONSOLE_DB="postgres://user:pass@host:5432/console?sslmode=require"
export CONSOLE_STATUS_PLUGINS="$PWD/bin/console-plugin-cloudflare,$PWD/bin/console-plugin-heroku"
export CONSOLE_NOTIFY_PLUGINS=$PWD/bin/console-plugin-slack
export CONSOLE_LLM_PLUGIN=$PWD/bin/console-plugin-anthropic
./console serve
```

**Windows (PowerShell):**

```powershell
$env:CONSOLE_STORE_PLUGIN   = "$PWD\bin\console-plugin-postgres.exe"
$env:CONSOLE_DB             = "postgres://user:pass@host:5432/console?sslmode=require"
$env:CONSOLE_STATUS_PLUGINS = "$PWD\bin\console-plugin-cloudflare.exe,$PWD\bin\console-plugin-heroku.exe"
$env:CONSOLE_NOTIFY_PLUGINS = "$PWD\bin\console-plugin-slack.exe"
$env:CONSOLE_LLM_PLUGIN     = "$PWD\bin\console-plugin-anthropic.exe"
.\console.exe serve
```

`CONSOLE_STATUS_PLUGINS` and `CONSOLE_NOTIFY_PLUGINS` accept a comma- (or
space-) separated list. See [plugins-architecture.md](plugins-architecture.md)
for what each plugin does and which provider-specific variables it reads.

## Tests

`go test ./...` runs everything. A couple of suites are gated on credentials and
**skip** by default:

- **Postgres store** — set `CONSOLE_TEST_POSTGRES_DSN` to a reachable database to
  run the integration tests; unset, they skip.

Everything else (engines, plugin gRPC round-trips, provider/notifier/LLM
adapters) runs with no external services, using `httptest` and in-memory fakes.

## Regenerating gRPC stubs

Only needed if you edit a `proto/*.proto`. Install the toolchain:

- **macOS:** `brew install protobuf`
- **Linux:** your package manager's `protobuf-compiler`, or a release from
  <https://github.com/protocolbuffers/protobuf/releases>
- **Windows:** `scoop install protobuf` or `choco install protoc`

Then the Go generators (any OS):

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
# ensure "$(go env GOPATH)/bin" (Windows: %GOPATH%\bin) is on PATH
```

Regenerate with `make proto` (macOS/Linux) or run the `protoc` command from the
Makefile's `proto` target directly. Commit the regenerated `*.pb.go`.

## Cross-compiling

Because Console is cgo-free, you can build for any platform from any platform by
setting `GOOS`/`GOARCH` with `CGO_ENABLED=0`:

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o console.exe ./cmd/console
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o console     ./cmd/console
```

The release bundles (see GitHub Releases) ship `console` plus all plugins for
darwin and linux on amd64 and arm64.
