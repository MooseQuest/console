# Observability

Console exposes its own health as Prometheus-format metrics, structured logs
with request IDs, and an opt-in pprof listener. This page is the operator
reference; the design is in [design/apm.md](design/apm.md). Nothing leaves the
process unless you scrape it or configure an exporter.

## Security warning: `/metrics` exposure

**`/metrics` is unauthenticated while built-in auth is off** (as with the rest
of the API today), and it follows the main listener's bind address, which is
loopback (`127.0.0.1:8080`) by default. Metrics contain no flag keys, subject
keys, or config values, but they do reveal route names, component provider
names, traffic levels, and the Console version.

- Do not expose `/metrics` to untrusted networks. If Console sits behind a
  reverse proxy, **block `/metrics` at the proxy** (or only allow your
  Prometheus server's address).
- Alternatively set `CONSOLE_METRICS_ADDR` to serve metrics on a **separate
  address** (for example `127.0.0.1:9091`) and keep it off public interfaces.
- Once built-in auth ships, `/metrics` requires the **`read` scope**
  ([design/auth.md](design/auth.md) section 3.4). Prometheus must then present a
  bearer token, see the scrape config below. Guidance: a separate metrics
  listener should be bound to loopback; a non-loopback metrics address should
  be protected by the same token check. This is not yet enforced: `serve` does
  a plain listen on `CONSOLE_METRICS_ADDR` with no loopback check (enforcement
  lands with #80), so choose the address deliberately.

See also [security/runtime-hardening.md](security/runtime-hardening.md).

## Configuration

| Env | Default | Meaning |
|---|---|---|
| `CONSOLE_METRICS` | `on` | Serve `/metrics`. `off`, `false`, `0`, or `no` disables it. |
| `CONSOLE_METRICS_ADDR` | *(same listener)* | Serve metrics on a separate address instead of the main listener. |
| `CONSOLE_PPROF_ADDR` | *(off)* | Enable `net/http/pprof` on this address (separate listener; loopback only unless overridden). |
| `CONSOLE_PPROF_ALLOW_REMOTE` | *(unset)* | Set to `1` (also `true`, `yes`, `on`) to allow a non-loopback `CONSOLE_PPROF_ADDR`. pprof has no authentication; see the hardening doc. |
| `CONSOLE_LOG_FORMAT` | `text` | `text` or `json` (`log/slog`). Any other value fails startup. |
| `CONSOLE_OTLP_ENDPOINT` | *(off)* | Enable OTLP export. Only honored by the optional `console-otel` build (nested module `contrib/otel`, see design 4.1.1); the default binary does not link OpenTelemetry and ignores it. |

Every response carries an `X-Request-Id` header (an incoming one is reused),
and the same ID appears in request logs. Secrets and component `config` values
are never logged.

## Prometheus scrape config

While auth is off (loopback, or a trusted network):

```yaml
scrape_configs:
  - job_name: console
    static_configs:
      - targets: ["127.0.0.1:8080"]
```

Once auth is enabled (#72, #80), `/metrics` requires a bearer token with the
`read` scope. Create one with `console auth token` and supply it:

```yaml
scrape_configs:
  - job_name: console
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/console-read-token
    static_configs:
      - targets: ["console.internal:8080"]
```

Prefer `credentials_file` over inline `credentials` so the token stays out of
config files in version control. The OpenTelemetry Collector's Prometheus
receiver accepts the same `authorization` block.

## Metrics reference

Histograms use fixed buckets (seconds): 0.005, 0.01, 0.025, 0.05, 0.1, 0.25,
0.5, 1, 2.5, 5, 10, +Inf, and are exposed as `_bucket`, `_sum`, and `_count`.
Route labels are the registered mux pattern (for example
`GET /api/flags/{key}`), never the raw path.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `console_http_requests_total` | counter | `route`, `method`, `code` | HTTP requests handled. |
| `console_http_request_duration_seconds` | histogram | `route` | HTTP request latency. |
| `console_http_in_flight` | gauge | | Requests currently being served. |
| `console_store_op_duration_seconds` | histogram | `op` | `store.Store` operation latency; `op` is the method name. |
| `console_store_errors_total` | counter | `op` | Failed store operations (not-found is not counted as an error). |
| `console_check_duration_seconds` | histogram | `provider` | Status provider check duration. |
| `console_check_results_total` | counter | `provider`, `state` | Check results; `state` is `operational`, `degraded`, `down`, or `unknown`. |
| `console_flag_evaluations_total` | counter | `result` | Flag evaluations; `result` is `on`, `off`, `not_found`, or `error`. |
| `console_flag_eval_duration_seconds` | histogram | | Flag evaluation latency. |
| `console_notify_sent_total` | counter | `sink`, `outcome` | Notification attempts; `outcome` is `ok` or `error`. |
| `go_goroutines` | gauge | | Live goroutines. |
| `go_memstats_heap_alloc_bytes` | gauge | | Bytes of allocated heap objects. |
| `go_gc_pause_seconds` | gauge | | Cumulative GC stop-the-world pause time; use `rate()`. |
| `console_build_info` | gauge | `version` | Always 1; carries the running version. |

## Dashboard

Import [grafana/console-dashboard.json](grafana/console-dashboard.json) in
Grafana (Dashboards, New, Import) and pick your Prometheus data source. It has
a `route` variable and panels for request rate, 5xx ratio, latency
percentiles, in-flight requests, store latency and errors, status check
duration and results, flag evaluations, notifications, and Go runtime stats.

## Alert rules

[prometheus/console-alerts.yml](prometheus/console-alerts.yml) ships four rules.
Thresholds are **tunable defaults**, not SLOs.

| Alert | Default condition |
|---|---|
| `ConsoleHighLatencyP99` | p99 request latency over 1s for 10m |
| `ConsoleHigh5xxRate` | 5xx ratio over 5% for 5m |
| `ConsoleStatusProviderFailing` | over half of a provider's checks `down` for 10m |
| `ConsoleStoreErrors` | any store errors for 5m |

Reference it from `prometheus.yml` with `rule_files:`.

## Validating these assets

```sh
scripts/check-observability.sh      # JSON validity, metric names, env vars
promtool check rules docs/prometheus/console-alerts.yml
```

The script verifies that the dashboard is valid JSON, that every metric used in
the dashboard and rules exists in code and in this page, and that every
`CONSOLE_*` variable in the design doc table is documented here. It runs
`promtool check rules` automatically when `promtool` is installed; CI runs it
on every push. If promtool is not available, run it manually as above.
