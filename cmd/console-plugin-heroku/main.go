// Command console-plugin-heroku is a Console status-provider plugin that
// reports the health of a Heroku app from the state of its dynos, served over
// gRPC (hashicorp/go-plugin). The host (console) launches it as a subprocess;
// it is not run directly.
//
// It reads its default API token from HEROKU_API_KEY (inherited from the host);
// per-component "api_token" config overrides it.
package main

import (
	"os"

	"github.com/moosequest/console/internal/plugin"
	"github.com/moosequest/console/internal/status/heroku"
)

func main() {
	plugin.ServeStatusProvider(heroku.New(heroku.WithToken(os.Getenv("HEROKU_API_KEY"))))
}
