// Command console-plugin-postgres is a Console storage-backend plugin that
// serves a Postgres-backed store.Store over gRPC (hashicorp/go-plugin). The
// host (console) launches it as a subprocess; it is not run directly.
//
// It reads its connection string from CONSOLE_DB (inherited from the host) and
// must be a postgres:// URL.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/moosequest/console/internal/plugin"
	"github.com/moosequest/console/internal/store/postgres"
)

func main() {
	dsn := os.Getenv("CONSOLE_DB")
	st, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "console-plugin-postgres:", err)
		os.Exit(1)
	}
	plugin.Serve(st)
}
