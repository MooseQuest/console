// Command console-plugin-cloudflare is a Console status-provider plugin that
// reports the health of Cloudflare Workers from their invocation analytics,
// served over gRPC (hashicorp/go-plugin). The host (console) launches it as a
// subprocess; it is not run directly.
//
// It reads its default API token from CLOUDFLARE_API_TOKEN (inherited from the
// host); per-component "api_token" config overrides it.
package main

import (
	"os"

	"github.com/moosequest/console/internal/plugin"
	"github.com/moosequest/console/internal/status/cloudflare"
)

func main() {
	plugin.ServeStatusProvider(cloudflare.New(cloudflare.WithToken(os.Getenv("CLOUDFLARE_API_TOKEN"))))
}
