# Plugins

Console is built to be extended. Four small Go interfaces are the seams:

| Seam | Interface | Default | What it controls |
|---|---|---|---|
| Storage | `store.Store` | SQLite | where flags, components, and checks are persisted |
| Status | `status.Provider` | `http` | how a component's health is checked |
| LLM | `llm.Provider` | none (Human mode) | the model behind AI-Assisted onboarding |
| Notify | `notify.Notifier` | none (no sink) | where status/flag events are delivered |

Each seam is extended **out-of-process**: a plugin is a separate
`console-plugin-<name>` executable that the `console` host launches as a
subprocess and talks to over gRPC ([hashicorp/go-plugin](https://github.com/hashicorp/go-plugin),
with AutoMTLS on). Nothing is hand-wired into the core at the composition root —
you build a plugin binary and point the host at it via the seam's
`CONSOLE_*_PLUGIN(S)` variable.

This page is the **authoring guide**: how to write a new plugin. For the full
mechanics — the gRPC contract, the host loaders, the per-plugin catalog and the
config each plugin reads, and how to run them — see the canonical
[plugin architecture](plugins-architecture.md).

- [Writing a plugin](#writing-a-plugin)
- [The Serve template](#the-serve-template)
- [Conventions](#conventions)

## Writing a plugin

A new plugin is the same four steps for every seam:

1. **(New seam only)** define a gRPC service in `proto/` and run `make proto`.
   The four existing seams (store, status, notify, llm) already have contracts —
   skip this unless you are adding a fifth seam.
2. **(New seam only)** implement the client + server adapters in
   `internal/plugin` that bridge the Go interface to the generated service (the
   store adapters are the template). Again, skip for the existing seams.
3. **Implement the seam interface.** Put a new backend or provider in its own
   subpackage — `internal/store/<name>`, `internal/status/<name>`,
   `internal/notify/<name>`, or `internal/llm/<name>` — so it can carry its own
   dependencies. The four interfaces are:

   - `store.Store` — `FlagStore` + `ComponentStore` + `CheckStore` + `Ping` +
     `Close`. Safe for concurrent use; return `core.ErrNotFound` /
     `core.ErrConflict` from lookups / creates.
   - `status.Provider` — `Name()` and `Check(ctx, comp) core.Check`.
   - `notify.Notifier` — `Name()` and `Notify(ctx, ev core.Event) error`. Return
     promptly and tolerate concurrent calls.
   - `llm.Provider` — `Name()` and `Complete(ctx, req) (string, error)`.

4. **Add a `cmd/console-plugin-<name>`** that constructs your implementation and
   calls the seam's `Serve` helper, then point the host at it via the seam's
   variable (`CONSOLE_STORE_PLUGIN`, `CONSOLE_STATUS_PLUGINS`,
   `CONSOLE_NOTIFY_PLUGINS`, or `CONSOLE_LLM_PLUGIN`). Run `make plugins` to build
   it into `./bin`.

The host loads it at startup — there is no core code to change. See
[plugin architecture](plugins-architecture.md) for how `loadPlugins` discovers
and launches each plugin.

## The Serve template

Each seam exposes a `Serve` helper in `internal/plugin`; your plugin's `main`
reads its config from the inherited environment and hands the implementation to
that helper. The helper takes over the process (it speaks the go-plugin
handshake) — the binary is never run directly, only launched by the host.

| Seam | Serve helper |
|---|---|
| Storage | `plugin.Serve(store.Store)` |
| Status | `plugin.ServeStatusProvider(status.Provider)` |
| Notify | `plugin.ServeNotifier(notify.Notifier)` |
| LLM | `plugin.ServeLLM(llm.Provider)` |

A notifier plugin, for example, is just:

```go
// Command console-plugin-slack delivers events to Slack via an Incoming Webhook,
// served over gRPC. The host launches it as a subprocess; it is not run directly.
package main

import (
	"fmt"
	"os"

	"github.com/moosequest/console/internal/notify/slack"
	"github.com/moosequest/console/internal/plugin"
)

func main() {
	url := os.Getenv("CONSOLE_SLACK_WEBHOOK_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-slack: CONSOLE_SLACK_WEBHOOK_URL is required")
		os.Exit(1)
	}
	plugin.ServeNotifier(slack.New(url))
}
```

The other seams follow the same shape: read your config from the environment,
construct the implementation, and call the matching `Serve*` helper. The host
inherits its environment to the subprocess, so provider-specific config
(`CONSOLE_DB`, `ANTHROPIC_API_KEY`, etc.) reaches your plugin unchanged.

## Conventions

- Match the existing **doc-comment style**: every exported type and function has
  a comment starting with its name.
- **Stdlib-first, minimal dependencies.** Pull in a third-party package only when
  it earns its place, and keep it inside the plugin's own subpackage.
- **Keep the binary cgo-free** — prefer pure-Go drivers (the SQLite backend uses
  `modernc.org/sqlite` for exactly this reason).
- **Add tests.** The existing engines and providers are table-tested with fakes
  against the narrow interfaces; new plugins should be too.

See [architecture](architecture.md) for how the seams fit into the whole,
[plugin architecture](plugins-architecture.md) for the gRPC plumbing and plugin
catalog, and [CONTRIBUTING](../CONTRIBUTING.md) for build/test/PR expectations.
