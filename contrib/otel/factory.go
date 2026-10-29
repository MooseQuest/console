package otel

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

// Factory is the Recorder factory for console-otel. It is activated by the
// CONSOLE_OTLP_ENDPOINT variable; when that is unset (or CONSOLE_METRICS is
// off) it defers to [app.DefaultRecorder], so console-otel behaves exactly
// like console. A bad endpoint is reported on warn and also falls back to the
// default recorder, so a typo never prevents Console from starting.
type Factory struct {
	getenv func(string) string
	warn   io.Writer

	mu   sync.Mutex
	recs []*Recorder
}

// NewFactory returns a Factory reading configuration from getenv and writing
// warnings to warn.
func NewFactory(getenv func(string) string, warn io.Writer) *Factory {
	return &Factory{getenv: getenv, warn: warn}
}

// Recorder is the func to assign to cli.Options.Recorder.
func (f *Factory) Recorder(cfg config.Config) obs.Recorder {
	endpoint := strings.TrimSpace(f.getenv(EndpointEnv))
	if endpoint == "" || !cfg.Metrics {
		return app.DefaultRecorder(cfg)
	}
	rec, err := New(context.Background(), Config{
		Endpoint:    endpoint,
		ServiceName: strings.TrimSpace(f.getenv("OTEL_SERVICE_NAME")),
		Version:     cfg.Version,
	})
	if err != nil {
		fmt.Fprintf(f.warn, "console-otel: %v; falling back to the default recorder\n", err)
		return app.DefaultRecorder(cfg)
	}
	f.mu.Lock()
	f.recs = append(f.recs, rec)
	f.mu.Unlock()
	return rec
}

// Shutdown flushes and stops every OTel Recorder the factory created. Call it
// after cli.Main returns.
func (f *Factory) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	recs := f.recs
	f.recs = nil
	f.mu.Unlock()
	var first error
	for _, r := range recs {
		if err := r.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}
