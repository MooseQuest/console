// Command console-plugin-webhook is a Console notifier plugin that delivers
// events to an arbitrary HTTP endpoint as a JSON POST, served over gRPC
// (hashicorp/go-plugin). The host (console) launches it as a subprocess; it is
// not run directly.
//
// It reads the endpoint from CONSOLE_WEBHOOK_URL (required) and, optionally, a
// shared secret from CONSOLE_WEBHOOK_SECRET (sent in the X-Webhook-Secret
// header). It exits if the URL is not set.
package main

import (
	"fmt"
	"os"

	"github.com/moosequest/console/internal/notify/webhook"
	"github.com/moosequest/console/internal/plugin"
)

func main() {
	url := os.Getenv("CONSOLE_WEBHOOK_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-webhook: CONSOLE_WEBHOOK_URL is required")
		os.Exit(1)
	}

	var opts []webhook.Option
	if secret := os.Getenv("CONSOLE_WEBHOOK_SECRET"); secret != "" {
		opts = append(opts, webhook.WithSecret(secret))
	}

	plugin.ServeNotifier(webhook.New(url, opts...))
}
