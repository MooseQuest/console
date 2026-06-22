// Command console-plugin-pagerduty is a Console notifier plugin that pages
// PagerDuty via the Events API v2, served over gRPC (hashicorp/go-plugin). The
// host (console) launches it as a subprocess; it is not run directly.
//
// It reads the service integration (routing) key from
// CONSOLE_PAGERDUTY_ROUTING_KEY (inherited from the host) and exits if it is
// not set.
package main

import (
	"fmt"
	"os"

	"github.com/moosequest/console/internal/notify/pagerduty"
	"github.com/moosequest/console/internal/plugin"
)

func main() {
	key := os.Getenv("CONSOLE_PAGERDUTY_ROUTING_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-pagerduty: CONSOLE_PAGERDUTY_ROUTING_KEY is required")
		os.Exit(1)
	}
	plugin.ServeNotifier(pagerduty.New(key))
}
