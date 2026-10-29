package otel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// EndpointEnv is the environment variable that activates the OTel recorder in
// the console-otel binary. It is the base OTLP/HTTP URL of a collector or
// backend (for example "http://localhost:4318"); "/v1/traces" and
// "/v1/metrics" are appended.
const EndpointEnv = "CONSOLE_OTLP_ENDPOINT"

// Config configures [New].
type Config struct {
	// Endpoint is the base OTLP/HTTP URL (scheme required: http or https).
	Endpoint string
	// ServiceName is the service.name resource attribute (default "console").
	ServiceName string
	// Version is the service.version resource attribute.
	Version string
	// MetricInterval is the periodic metric export interval (default 15s).
	MetricInterval time.Duration
}

// New builds a Recorder exporting traces and metrics to cfg.Endpoint over
// OTLP/HTTP. The caller must call [Recorder.Shutdown] to flush on exit.
func New(ctx context.Context, cfg Config) (*Recorder, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return nil, fmt.Errorf("otel: %s must be an http:// or https:// URL, got %q", EndpointEnv, cfg.Endpoint)
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "console"
	}
	if cfg.MetricInterval <= 0 {
		cfg.MetricInterval = 15 * time.Second
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
	))
	if err != nil {
		return nil, fmt.Errorf("otel: resource: %w", err)
	}

	texp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(base+"/v1/traces"))
	if err != nil {
		return nil, fmt.Errorf("otel: trace exporter: %w", err)
	}
	mexp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(base+"/v1/metrics"))
	if err != nil {
		return nil, fmt.Errorf("otel: metric exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(texp, sdktrace.WithBatchTimeout(2*time.Second)),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp, sdkmetric.WithInterval(cfg.MetricInterval))),
	)
	return NewWithProviders(tp, mp), nil
}
