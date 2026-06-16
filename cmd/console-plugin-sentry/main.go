// Command console-plugin-sentry is a Console status-provider plugin that
// reports the health of a Sentry project from its unresolved issue volume,
// served over gRPC (hashicorp/go-plugin). The host (console) launches it as a
// subprocess; it is not run directly.
//
// It reads its default auth token from SENTRY_AUTH_TOKEN (inherited from the
// host); per-component "auth_token" config overrides it.
package main

import (
	"os"

	"github.com/moosequest/console/internal/plugin"
	"github.com/moosequest/console/internal/status/sentry"
)

func main() {
	plugin.ServeStatusProvider(sentry.New(sentry.WithToken(os.Getenv("SENTRY_AUTH_TOKEN"))))
}
