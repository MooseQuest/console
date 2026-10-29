// Command console is the Console CLI: a single static binary that serves the
// dashboard + API, manages feature flags and status components, and runs the
// onboarding assistant (Human and AI-Assisted modes). It is a thin shim over
// internal/cli.
package main

import (
	"os"

	"github.com/moosequest/console/internal/cli"
)

// version is the release version. Release builds stamp it via
// -ldflags "-X main.version=<tag>" (see the Makefile / scripts/dist.sh).
var version = "dev"

func main() {
	os.Exit(cli.Main(cli.Options{Version: version}))
}
