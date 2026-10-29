// Command console-otel is the Console CLI built with the OpenTelemetry
// Recorder. It is identical to cmd/console except that setting
// CONSOLE_OTLP_ENDPOINT (for example http://localhost:4318) exports Console's
// metrics and traces over OTLP/HTTP. With the variable unset it behaves exactly
// like console. See contrib/otel/README.md.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/moosequest/console/contrib/otel"
	"github.com/moosequest/console/internal/cli"
)

// version is the release version, stamped via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	f := otel.NewFactory(os.Getenv, os.Stderr)
	code := cli.Main(cli.Options{Version: version, Recorder: f.Recorder})

	// Flush buffered spans and metrics before exiting.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "console-otel: flush: %v\n", err)
	}
	os.Exit(code)
}
