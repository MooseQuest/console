// Package obs is Console's instrumentation seam: a small, stdlib-only API for
// recording counters, gauges, histograms, and spans about Console's own
// behavior (application performance monitoring, see docs/design/apm.md).
//
// Instrumented code depends only on the [Recorder] interface. [Nop] is the
// zero-cost default used when metrics are disabled; [Memory] is an in-process
// registry that renders the Prometheus text exposition format. The concrete
// Recorder is chosen in internal/app (the composition root) and exposed as
// App.Obs, which is always non-nil.
//
// # Label cardinality
//
// Every distinct label-value combination creates a new time series that lives
// for the life of the process, so label values must come from a small, bounded
// set. Use registered route patterns ("GET /api/flags/{key}"), HTTP methods,
// status classes, operation names, and provider kinds. Never use raw URL paths,
// flag keys, subject keys, component IDs, request IDs, error strings, or any
// other user-controlled or unbounded value. As a backstop, [Memory] caps each
// metric name at [MaxSeriesPerMetric] series; instruments requested past the
// cap still work but are not exported.
//
// # Histogram buckets
//
// Histograms use the fixed [DefaultBuckets] (upper bounds, in seconds):
// 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, plus +Inf.
package obs

import "context"

// Label is a single metric dimension. Names must be valid Prometheus label
// names ([a-zA-Z_][a-zA-Z0-9_]*); values are arbitrary but must be bounded
// (see the package documentation).
type Label struct {
	Name  string
	Value string
}

// L is a shorthand constructor for a [Label].
func L(name, value string) Label { return Label{Name: name, Value: value} }

// Counter is a monotonically increasing value.
type Counter interface {
	// Inc adds 1.
	Inc()
	// Add adds v. Negative values are ignored to preserve monotonicity.
	Add(v float64)
}

// Gauge is a value that can go up and down.
type Gauge interface {
	// Set replaces the value.
	Set(v float64)
	// Add adds v (which may be negative).
	Add(v float64)
	// Inc adds 1.
	Inc()
	// Dec subtracts 1.
	Dec()
}

// Histogram samples observations into fixed buckets.
type Histogram interface {
	// Observe records one observation, conventionally in seconds.
	Observe(v float64)
}

// Recorder is the instrumentation seam. The zero-cost default is [Nop].
// Implementations must be safe for concurrent use. Requesting the same name
// and labels repeatedly returns instruments backed by the same series.
type Recorder interface {
	// Counter returns the counter for name and labels.
	Counter(name string, labels ...Label) Counter
	// Histogram returns the histogram for name and labels.
	Histogram(name string, labels ...Label) Histogram
	// Gauge returns the gauge for name and labels.
	Gauge(name string, labels ...Label) Gauge
	// StartSpan returns a derived context and a func that ends the span,
	// recording err (which may be nil) as its outcome.
	StartSpan(ctx context.Context, name string, labels ...Label) (context.Context, func(err error))
}
