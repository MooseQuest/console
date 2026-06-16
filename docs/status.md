# Status monitoring

Console's status engine watches the parts of your application and rolls their
health up into a single snapshot suitable for a status page or the dashboard
overview.

- [Components](#components)
- [Providers](#providers)
- [The http provider](#the-http-provider)
- [The cloudflare-workers provider](#the-cloudflare-workers-provider)
- [Checks and health states](#checks-and-health-states)
- [Snapshot aggregation](#snapshot-aggregation)
- [CLI](#cli)
- [HTTP API](#http-api)

## Components

A **component** is a monitored part of your app — an API, a worker, a database.
Each component names the **provider** that checks it and carries a free-form
config map the provider interprets.

```jsonc
{
  "key": "api",                 // unique identifier
  "name": "Public API",         // display name
  "description": "",
  "provider": "http",           // which status.Provider checks it
  "config": { "url": "https://example.com/health" }
}
```

## Providers

A provider performs a single health check and returns its state. Providers are
registered by name; a component selects one via its `provider` field. Console
ships with the built-in **`http`** provider and a **`cloudflare-workers`**
provider. Additional providers (TCP, a custom probe, a downstream dependency
check) are added by implementing the `status.Provider` interface and registering
them — see [plugins](plugins.md).

If a component names a provider that isn't registered, its check is recorded as
`unknown` with a message — it is still recorded so the snapshot reflects reality.

## The http provider

The `http` provider issues an HTTP request and maps the response to a health
state. It reads these keys from the component's `config`:

| Key | Required | Default | Meaning |
|---|---|---|---|
| `url` | yes | — | endpoint to probe |
| `method` | no | `GET` | request method |
| `expect_status` | no | — | exact status code that means *operational* |
| `timeout` | no | `5s` | per-probe timeout (Go duration string, e.g. `2s`) |

State mapping:

| Outcome | State |
|---|---|
| 2xx, or the exact `expect_status` when set | **operational** |
| any other HTTP response received | **degraded** |
| connection error or timeout | **down** |
| missing `url` | **unknown** |

> When `expect_status` is set, *only* that exact code is operational — a 2xx
> that doesn't match it is reported as degraded (`status 204, expected 200`).

Example component using a custom method, expected code, and timeout:

```jsonc
{
  "key": "checkout",
  "name": "Checkout service",
  "provider": "http",
  "config": {
    "url": "https://example.com/healthz",
    "method": "GET",
    "expect_status": "200",
    "timeout": "2s"
  }
}
```

## The cloudflare-workers provider

> Ships as the out-of-process **`console-plugin-cloudflare`** plugin — set
> `CONSOLE_STATUS_PLUGINS` to its path (see
> [plugin architecture](plugins-architecture.md)). The config keys below are set
> on the component as usual.

The `cloudflare-workers` provider reports a Cloudflare Worker's health from its
recent invocation analytics. It queries the Cloudflare GraphQL Analytics API
(`workersInvocationsAdaptive`) for the request and error counts over a trailing
window and maps the error rate to a state.

Config keys:

| Key | Required | Default | Meaning |
|---|---|---|---|
| `account_id` | yes | — | Cloudflare account tag |
| `worker` | yes | — | Worker script name (alias: `script`) |
| `api_token` | no | `CLOUDFLARE_API_TOKEN` | per-component token override |
| `window` | no | `15m` | trailing window (Go duration) |
| `degraded_pct` | no | `1` | error-rate % at/above which state is degraded |
| `down_pct` | no | `5` | error-rate % at/above which state is down |
| `timeout` | no | `10s` | API call timeout |

State mapping (error rate = errors / requests over the window):

| Condition | State |
|---|---|
| rate `< degraded_pct` | **operational** |
| `degraded_pct ≤` rate `< down_pct` | **degraded** |
| rate `≥ down_pct` | **down** |
| Cloudflare API/network failure or GraphQL error | **down** |
| missing `account_id` / `worker` / token | **unknown** |
| zero invocations in the window (idle) | **unknown** |

```bash
console status add my-api -provider cloudflare-workers
# then set its config (account_id, worker) via the API, or:
export CLOUDFLARE_API_TOKEN=...   # used as the default token
```

```json
{
  "key": "my-api",
  "name": "My Worker",
  "provider": "cloudflare-workers",
  "config": { "account_id": "22b5…", "worker": "my-api", "window": "30m" }
}
```

## Checks and health states

A **check** is one point-in-time observation:

```jsonc
{
  "component": "api",
  "state": 1,                  // see the states below
  "message": "status 200",
  "latency": 41200000,         // nanoseconds (Go time.Duration)
  "checked_at": "2026-06-15T12:00:00Z"
}
```

There are four health states. Their ordering matters for aggregation:

| State (JSON int) | Name | Meaning |
|---|---|---|
| `0` | `unknown` | not yet checked / no `url` / unknown provider |
| `1` | `operational` | healthy |
| `2` | `degraded` | working but impaired |
| `3` | `down` | not working |

The dashboard renders these as colored dots: green (operational), amber
(degraded), red (down), grey (unknown).

## Snapshot aggregation

A **snapshot** reads the *latest* check per component and aggregates them into
one overall `core.Health`:

- The overall state is the **worst (highest-severity)** state across components —
  worst-wins.
- **Exception:** `unknown` is treated as *least*-severe, ranked below
  `operational`, so a single not-yet-checked component can never mask a real
  `down`.
- With **zero** components/checks, the overall state is `unknown`.

So a system with one `down` component and one `unknown` component reports
`down`; a system with everything `operational` and one `unknown` reports
`operational`.

```bash
console status snapshot
# overall: down
#   api      operational  status 200
#   worker   down         request failed: ...
#   cache    unknown      no url configured
```

## CLI

```text
console status list
console status add <key> -url <url> [-name -provider]
console status check [<key>]      # one component, or all when omitted
console status snapshot
console status delete <key>
```

```bash
console status add web -url https://example.com -name "Web app"
console status add api -url https://example.com/health -name "Public API" -provider http
console status check                 # check every component
console status check api             # check just one
console status snapshot
console status delete web
```

## HTTP API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Aggregate health snapshot |
| `GET` | `/api/components` | List components |
| `POST` | `/api/components` | Create a component |
| `GET` | `/api/components/{key}` | Get a component |
| `PUT` | `/api/components/{key}` | Update a component |
| `DELETE` | `/api/components/{key}` | Delete a component |
| `POST` | `/api/components/{key}/check` | Run a check now |

```bash
# Register a component
curl -X POST localhost:8080/api/components \
  -d '{"key":"api","name":"Public API","provider":"http","config":{"url":"https://example.com/health"}}'

# Run a check now (returns the resulting Check)
curl -X POST localhost:8080/api/components/api/check

# Read the aggregate snapshot
curl localhost:8080/api/health
```

See the full [API reference](api.md) for response shapes and error codes.
