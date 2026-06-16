// Command console-plugin-slack is a Console notifier plugin that delivers events
// to Slack via an Incoming Webhook, served over gRPC (hashicorp/go-plugin). The
// host (console) launches it as a subprocess; it is not run directly.
//
// It reads the webhook URL from CONSOLE_SLACK_WEBHOOK_URL (inherited from the
// host) and exits if it is not set.
package main

import (
	"fmt"
	"os"

	"github.com/moosequest/console/internal/notify/slack"
	"github.com/moosequest/console/internal/plugin"
)

func main() {
	url := os.Getenv("CONSOLE_SLACK_WEBHOOK_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-slack: CONSOLE_SLACK_WEBHOOK_URL is required")
		os.Exit(1)
	}
	plugin.ServeNotifier(slack.New(url))
}
