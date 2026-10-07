# APM harness — design

Status: proposed · Target: v0.7 · Owner: Kristerpher Henderson

## 1. Problem

Console has no visibility into its own behavior. Operators cannot see request
latency, error rates, store or status-provider timings, or Go runtime health,
and maintainers have no way to find performance regressions. Request logging
(`internal/server`) is the only signal today.

This document covers **application performance monitoring (APM)** of a running
Console instance. It does not cover product-usage analytics (who runs Console,
which features they use); that is a separate, opt-in decision.

## 2. Goals and non-goals

Goals
- RED metrics (rate, errors, duration) for every HTTP route.
- Timings and error counts for `store.Store`, `status.Provider` checks, flag
  evaluation, and notifier dispatch.
- Go runtime stats (goroutines, heap, GC pause).
- A Prometheus-format `/metrics` endpoint and an opt-in pprof listener.
- Request IDs in logs; structured logging via `log/slog`.
- Optional distributed tracing and OTLP export, without burdening the core.
- A Grafana dashboard and alert rules shipped in `docs/`.

Non-goals
- Replacing `status.Provider` (that monitors *other* services; this monitors
  Console).
- Hosting a metrics backend, or a hosted telemetry service.
- Phoning home. Nothing leaves the process unless the operator configures it.

## 3. Constraints (from CLAUDE.md)

- **cgo-free, static binary.** Every dependency must be pure Go.
- **Stdlib-first.** The default implementation uses only the standard library:
  no Prometheus client, no OTel in core.
- **Interfaces are the seams.** Instrumentation is a new seam wired only in
  `internal/app/app.go`.
- **`internal/core` stays dependency-free.** Metric types live in a new package.
- **Flag evaluation stays deterministic.** Instrumentation observes evaluation;
  it must never influence its result or add randomness.

## 4. Design

### 4.1 `internal/obs` package

```go
// Recorder is the instrumentation seam. The zero-cost default is Nop.
type Recorder interface {
    Counter(name string, labels ...Label) Counter
    Histogram(name string, labels ...Label) Histogram
    Gauge(name string, labels ...Label) Gauge
    // StartSpan returns a derived context and a func that ends the span.
    StartSpan(ctx context.Context, name string, labels ...Label) (context.Context, func(err error))
}
```

Implementations
- `obs.Nop` — default when observability is disabled; allocation-free.
- `obs.Memory` — stdlib in-process registry that renders Prometheus text
  exposition format (hand-rolled; counters, gauges, fixed-bucket histograms).
  Spans are no-ops that still propagate a request ID.
- `obs/otel` (optional, separate Go module or build-tagged package) — OTel SDK
  adapter exporting traces and metrics over OTLP. Not linked into the default
  binary. OTel is already an indirect dependency via grpc, and it is pure Go,
  so it satisfies the cgo-free rule; whether it lives in core is a decision to
  take when that card is picked up.

Label cardinality is bounded by construction: route labels use the registered
mux pattern (`GET /api/flags/{key}`), never the raw path; flag keys and
component IDs are **not** used as metric labels.

### 4.2 Wiring

`app.App` gains `Obs obs.Recorder` (always non-nil). `config` adds:

| Env | Default | Meaning |
|---|---|---|
| `CONSOLE_METRICS` | `on` | Serve `/metrics` |
| `CONSOLE_METRICS_ADDR` | *(same listener)* | Serve metrics on a separate address |
| `CONSOLE_PPROF_ADDR` | *(off)* | Enable `net/http/pprof` on this address |
| `CONSOLE_LOG_FORMAT` | `text` | `text` or `json` (slog) |
| `CONSOLE_OTLP_ENDPOINT` | *(off)* | Enable the OTel adapter, if built |

### 4.3 Instrumentation points

| Layer | Mechanism | Metrics |
|---|---|---|
| HTTP | middleware in `server.Handler()`, outermost | `console_http_requests_total{route,method,code}`, `console_http_request_duration_seconds{route}`, `console_http_in_flight` |
| Store | decorator `obs.WrapStore(store.Store, Recorder)` | `console_store_op_duration_seconds{op}`, `console_store_errors_total{op}` |
| Status | decorator around each `status.Provider` | `console_check_duration_seconds{provider}`, `console_check_results_total{provider,state}` |
| Flags | wrapper around the engine's evaluate entry point | `console_flag_evaluations_total{result}`, `console_flag_eval_duration_seconds` |
| Notify | dispatcher hook | `console_notify_sent_total{sink,outcome}` |
| Runtime | `runtime/metrics` sampled on scrape | goroutines, heap, GC pause, `console_build_info{version}` |

Decorators keep `store.Store` and `status.Provider` implementations (including
out-of-process plugins) unchanged and unaware of observability.

### 4.4 Logging

Replace ad-hoc `log.Printf` in the server middleware with `log/slog`, including
a request ID (taken from `X-Request-Id` if present, else generated) set on the
response and stored in the request context. Secrets and component `config`
values must never be logged.

### 4.5 Endpoints and exposure

`/metrics` and pprof are **unauthenticated**, like the rest of the API (see
[SECURITY.md](../../SECURITY.md)). Therefore:
- pprof is off by default and, when enabled, refuses a non-loopback address
  unless `CONSOLE_PPROF_ALLOW_REMOTE=1`.
- `/metrics` follows the main listener's bind address (loopback by default).
  Operators exposing Console behind a proxy should block `/metrics` at the
  proxy or move it to `CONSOLE_METRICS_ADDR`.
- Metrics contain no flag keys, subject keys, or config values.
- Document this in `docs/security/runtime-hardening.md`.

### 4.6 Performance budget

Instrumentation must add no more than ~1 µs and zero allocations per flag
evaluation on the hot path (benchmark in CI), and keep the default binary size
growth under ~200 KB.

## 5. Alternatives considered

- **Prometheus client library.** Mature, but adds a dependency tree for what is
  ~300 lines of text-format code at our scale. Revisit if histograms or
  exemplars outgrow the hand-rolled registry.
- **OTel in core.** Best ecosystem story, but large and moving. Kept optional.
- **expvar.** In stdlib but JSON-only and not Prometheus-compatible.

## 6. Delivery plan (cards)

1. `obs` package: `Recorder`, `Nop`, `Memory`, Prometheus text rendering.
2. HTTP middleware, `/metrics` endpoint, slog request logging + request IDs.
3. Store and status-provider decorators.
4. Flag-evaluation, notifier and runtime metrics.
5. pprof listener with loopback guard.
6. Optional OTel/OTLP adapter.
7. Grafana dashboard, alert rules, and docs (`docs/observability.md`, hardening doc).
8. Benchmarks guarding the performance budget.

## 7. Open questions

- Should `/metrics` default to the main listener or a separate address?
  (Proposed: main listener on loopback; separate address recommended in docs.)
- OTel adapter: build-tagged package in this module, or a separate plugin
  binary like the other seams?
- Do we want an opt-in anonymous usage ping as a separate follow-up? Out of scope here.
