# contrib/otel

An optional OpenTelemetry adapter for Console's `obs.Recorder` seam, plus a
`console-otel` binary. It is a separate Go module so the root module, and the
default `console` binary, never depend on `go.opentelemetry.io` (decision:
[docs/design/apm.md](../../docs/design/apm.md) section 4.1.1, option B).

> **`/metrics` is not served when OTLP is active.** With
> `CONSOLE_OTLP_ENDPOINT` set, Console pushes over OTLP instead and the
> Prometheus scrape endpoint is absent. Unset it to get `/metrics` back.

## Use

```sh
cd contrib/otel
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o console-otel ./cmd/console-otel
CONSOLE_OTLP_ENDPOINT=http://localhost:4318 ./console-otel serve
```

`CONSOLE_OTLP_ENDPOINT` is the base OTLP/HTTP URL of a Collector or backend;
`/v1/traces` and `/v1/metrics` are appended. Optional: `OTEL_SERVICE_NAME`
(default `console`).

- Unset endpoint: `console-otel` behaves exactly like `console` (default
  recorder, `/metrics` served).
- `CONSOLE_METRICS=off`: instrumentation stays off even if an endpoint is set.
- Invalid endpoint: a warning is printed and the default recorder is used.
- With an endpoint set, metrics and spans are pushed over OTLP and the
  Prometheus `/metrics` endpoint is not served (the recorder is not an
  `obs.Memory`).
- Metrics are exported every 15 s; spans are batched (2 s). Both are flushed on
  exit.
- The binary is several MB larger than `console` and is outside the
  instrumentation size budget (apm.md 4.6).

Metrics-only users do not need this module: the OpenTelemetry Collector can
scrape `/metrics` with a `read` token ([auth.md](../../docs/design/auth.md)).
The adapter is for traces and native OTLP push.

## Semantics

Counters ignore non-positive adds; gauges export the running value after every
change; histograms use `obs.DefaultBuckets` as explicit bounds. Series beyond
`obs.MaxSeriesPerMetric` per name, and instruments requested under a name already
registered as another kind, work but are not exported. `TestConformance` runs
one scenario against `obs.Memory` and this adapter and requires identical
output. Spans nest through the returned context, carry the Console request ID as
`request.id`, and record a non-nil error on end.

## Develop

`go install` of this module with a version suffix (`...@version`) is
**unsupported**: the `replace` directive in `go.mod` prevents it. Build
in-repo as shown above.

The module uses `replace github.com/moosequest/console => ../..` for in-repo
builds (it imports `internal/` packages, which Go allows because the module path
is under the root's). Run `go test -race ./...` from this directory; the root
`go build ./...` does not include it.
