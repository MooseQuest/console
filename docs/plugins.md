# Plugins

Console is built to be extended. Four small Go interfaces are the seams:

| Seam | Interface | Default | What it controls |
|---|---|---|---|
| Storage | `store.Store` | SQLite | where flags, components, and checks are persisted |
| Status | `status.Provider` | `http`, `cloudflare-workers` | how a component's health is checked |
| LLM | `llm.Provider` | Anthropic | the model behind AI-Assisted onboarding |
| Notify | `notify.Notifier` | Slack (when configured) | where status/flag events are delivered |

Adding a plugin is always the same two steps: **implement the interface**, then
**wire it in** at the composition root (`internal/app/app.go`). Nothing above the
seam needs to change.

- [Add a status provider](#add-a-status-provider)
- [Add a storage backend](#add-a-storage-backend)
- [Add an LLM provider](#add-an-llm-provider)
- [Add a notifier](#add-a-notifier)
- [Conventions](#conventions)

## Add a status provider

A status provider performs one health check and returns a `core.Check`.

```go
// Provider — internal/status/provider.go
type Provider interface {
    Name() string
    Check(ctx context.Context, comp core.Component) core.Check
}
```

Implement it (e.g. a TCP dial check):

```go
package status

import (
    "context"
    "net"
    "time"

    "github.com/moosequest/console/internal/core"
)

// TCPProvider reports operational when a TCP connection to config["addr"]
// succeeds, down when it fails.
type TCPProvider struct{}

func (p *TCPProvider) Name() string { return "tcp" }

func (p *TCPProvider) Check(ctx context.Context, comp core.Component) core.Check {
    check := core.Check{Component: comp.Key, CheckedAt: time.Now().UTC()}

    addr := comp.Config["addr"]
    if addr == "" {
        check.State = core.StateUnknown
        check.Message = "no addr configured"
        return check
    }

    timeout := 5 * time.Second
    if raw := comp.Config["timeout"]; raw != "" {
        if d, err := time.ParseDuration(raw); err == nil {
            timeout = d
        }
    }

    d := net.Dialer{Timeout: timeout}
    start := time.Now()
    conn, err := d.DialContext(ctx, "tcp", addr)
    check.Latency = time.Since(start)
    if err != nil {
        check.State = core.StateDown
        check.Message = err.Error()
        return check
    }
    _ = conn.Close()
    check.State = core.StateOperational
    check.Message = "connected"
    return check
}
```

Wire it in `internal/app/app.go` by registering it alongside the built-in HTTP
provider:

```go
a := &App{
    Config: cfg,
    Store:  st,
    Flags:  flags.New(st),
    Status: status.New(st, st, &status.HTTPProvider{}, &status.TCPProvider{}),
    LLM:    newLLM(cfg),
}
```

Now a component with `"provider": "tcp"` and `config.addr` is checked by your
provider. (You can also register at runtime with `engine.Register(p)`.) A
component naming an unregistered provider records an `unknown` check with a
message, so misconfiguration is visible rather than silent.

## Add a storage backend

> Storage backends now run **out-of-process** as gRPC plugins — see
> [plugins-architecture.md](plugins-architecture.md). The interface below is what
> a backend implements; it is then served from a `console-plugin-*` executable
> rather than compiled into the core. SQLite remains the built-in default.

A storage backend persists every domain object. The interface composes three
smaller ones:

```go
// Store — internal/store/store.go
type Store interface {
    FlagStore        // CreateFlag / GetFlag / ListFlags / UpdateFlag / DeleteFlag
    ComponentStore   // CreateComponent / GetComponent / ListComponents / UpdateComponent / DeleteComponent
    CheckStore       // RecordCheck / LatestCheck / LatestChecks
    Ping(ctx context.Context) error
    Close() error
}
```

Contract:

- Implementations must be **safe for concurrent use**.
- Lookups return `core.ErrNotFound` when a key is absent.
- Creates return `core.ErrConflict` when the key already exists.
- `RecordCheck` appends; `LatestCheck`/`LatestChecks` read back the most recent
  state per component.

Put a new backend in its own subpackage (mirroring `internal/store/sqlite`) so it
can carry its own driver dependency:

```go
package postgres

import (
    "context"

    "github.com/moosequest/console/internal/core"
)

type Store struct { /* *sql.DB, etc. */ }

func Open(ctx context.Context, dsn string) (*Store, error) { /* connect + migrate */ }

func (s *Store) CreateFlag(ctx context.Context, f core.Flag) error { /* ... */ }
func (s *Store) GetFlag(ctx context.Context, key string) (core.Flag, error) { /* ... return core.ErrNotFound */ }
// ...the rest of FlagStore, ComponentStore, CheckStore...
func (s *Store) Ping(ctx context.Context) error { /* ... */ }
func (s *Store) Close() error { /* ... */ }
```

Wire it in `internal/app/app.go` where the store is opened. The default opens
SQLite directly; to support a choice, branch on the config (you might add a
`CONSOLE_STORE`-style selector):

```go
st, err := sqlite.Open(ctx, cfg.DB)   // ← the existing line
// becomes, for example:
// var st store.Store
// switch cfg.Store {
// case "postgres":
//     st, err = postgres.Open(ctx, cfg.DB)
// default:
//     st, err = sqlite.Open(ctx, cfg.DB)
// }
```

Because the engines depend on `store.Store` (and its sub-interfaces), no engine,
server, or CLI code changes.

> Keep the binary **cgo-free**: prefer pure-Go drivers (the SQLite backend uses
> `modernc.org/sqlite` for exactly this reason).

## Add an LLM provider

The LLM seam is a single text completion, used only by AI-Assisted onboarding.

```go
// Provider — internal/llm/provider.go
type Provider interface {
    Name() string
    Complete(ctx context.Context, req Request) (string, error)
}

type Request struct {
    System    string
    Messages  []Message
    MaxTokens int
    Model     string
}
```

Implement it:

```go
package llm

import "context"

type OpenAI struct {
    apiKey string
    model  string
}

func NewOpenAI(apiKey string) *OpenAI { return &OpenAI{apiKey: apiKey, model: "gpt-4o-mini"} }

func (o *OpenAI) Name() string { return "openai" }

func (o *OpenAI) Complete(ctx context.Context, req Request) (string, error) {
    // call the provider's API; return the assistant reply text.
}
```

Wire it into `newLLM` in `internal/app/app.go`, which selects the provider from
`cfg.LLMProvider`:

```go
func newLLM(cfg config.Config) llm.Provider {
    switch cfg.LLMProvider {
    case "anthropic":
        var opts []llm.Option
        if cfg.Model != "" {
            opts = append(opts, llm.WithModel(cfg.Model))
        }
        return llm.NewAnthropic(cfg.AnthropicKey, opts...)
    case "openai":                      // ← new case
        return llm.NewOpenAI(/* key from cfg / env */)
    default:
        return nil                      // nil disables AI-Assisted mode
    }
}
```

Returning `nil` for an unknown provider is intentional: AI-Assisted onboarding
becomes unavailable and callers fall back to Human mode. Set
`CONSOLE_LLM_PROVIDER` to select your provider at runtime.

## Add a notifier

A notifier delivers a `core.Event` (a status transition or flag change) to an
external destination. The status and flag engines emit events; a
`notify.Dispatcher` fans each one out to every registered notifier.

```go
// Notifier — internal/notify/notify.go
type Notifier interface {
    Name() string
    Notify(ctx context.Context, ev core.Event) error
}
```

A webhook notifier, for example:

```go
// internal/notify/webhook/webhook.go
type Notifier struct{ URL string; HTTP *http.Client }

func (n *Notifier) Name() string { return "webhook" }

func (n *Notifier) Notify(ctx context.Context, ev core.Event) error {
    body, _ := json.Marshal(ev) // core.Event is JSON-tagged
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := n.client().Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode >= 300 { return fmt.Errorf("webhook: status %d", resp.StatusCode) }
    return nil
}
```

Wire it in `internal/app/app.go` inside `newNotify`, registering it when its
config is present:

```go
func newNotify(cfg config.Config) *notify.Dispatcher {
    d := notify.NewDispatcher()
    if cfg.SlackWebhookURL != "" {
        d.Register(slack.New(cfg.SlackWebhookURL))
    }
    if cfg.WebhookURL != "" {
        d.Register(webhook.New(cfg.WebhookURL))
    }
    return d
}
```

Notifiers should return promptly and tolerate being called concurrently. The
dispatcher bounds each call with a timeout and logs failures — a broken sink
never fails the operation that produced the event. See
[notifications](notifications.md) for the event model.

## Conventions

- Match the existing **doc-comment style**: every exported type and function has
  a comment starting with its name.
- **Stdlib-first, minimal dependencies.** Pull in a third-party package only when
  it earns its place, and keep it inside the plugin's own subpackage.
- **Keep the binary cgo-free** — prefer pure-Go drivers.
- **Add tests.** The existing engines and providers are table-tested with fakes
  against the narrow interfaces; new plugins should be too.

See [architecture](architecture.md) for how the seams fit into the whole, and
[CONTRIBUTING](../CONTRIBUTING.md) for build/test/PR expectations.
