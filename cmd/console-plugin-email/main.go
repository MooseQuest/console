// Command console-plugin-email is a Console notifier plugin that delivers events
// as plain-text email over SMTP, served over gRPC (hashicorp/go-plugin). The
// host (console) launches it as a subprocess; it is not run directly.
//
// It reads its configuration from the environment:
//
//	SMTP_HOST      SMTP server hostname              (required)
//	SMTP_PORT      SMTP server port                  (default "587")
//	SMTP_USERNAME  PLAIN-auth username, if any       (optional)
//	SMTP_PASSWORD  PLAIN-auth password, if any       (optional)
//	EMAIL_FROM     sender address                    (required)
//	EMAIL_TO       comma-separated recipient list     (required)
//
// It exits if any required value is missing.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/moosequest/console/internal/notify/email"
	"github.com/moosequest/console/internal/plugin"
)

func main() {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-email: SMTP_HOST is required")
		os.Exit(1)
	}
	from := os.Getenv("EMAIL_FROM")
	if from == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-email: EMAIL_FROM is required")
		os.Exit(1)
	}
	rawTo := os.Getenv("EMAIL_TO")
	if rawTo == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-email: EMAIL_TO is required")
		os.Exit(1)
	}

	var to []string
	for _, addr := range strings.Split(rawTo, ",") {
		if addr = strings.TrimSpace(addr); addr != "" {
			to = append(to, addr)
		}
	}
	if len(to) == 0 {
		fmt.Fprintln(os.Stderr, "console-plugin-email: EMAIL_TO is required")
		os.Exit(1)
	}

	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}

	plugin.ServeNotifier(email.New(email.Config{
		Host:     host,
		Port:     port,
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     from,
		To:       to,
	}))
}
