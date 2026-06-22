// Command console-plugin-discord is a Console notifier plugin that delivers
// events to a Discord channel via a channel Webhook, served over gRPC
// (hashicorp/go-plugin). The host (console) launches it as a subprocess; it is
// not run directly.
//
// It reads the webhook URL from CONSOLE_DISCORD_WEBHOOK_URL (inherited from the
// host) and exits if it is not set.
package main

import (
	"fmt"
	"os"

	"github.com/moosequest/console/internal/notify/discord"
	"github.com/moosequest/console/internal/plugin"
)

func main() {
	url := os.Getenv("CONSOLE_DISCORD_WEBHOOK_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-discord: CONSOLE_DISCORD_WEBHOOK_URL is required")
		os.Exit(1)
	}
	plugin.ServeNotifier(discord.New(url))
}
