package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/config"
)

const authUsage = `Manage API authentication.

Usage:
  console auth token
  console auth key create|list|revoke ...
  console auth audit list|prune ...

Subcommands:
  token   Print a freshly generated API token (csk_..., 256-bit) to stdout.
          Nothing is stored; place it in your secret store and configure it
          via CONSOLE_ADMIN_TOKENS / CONSOLE_WRITE_TOKENS / CONSOLE_READ_TOKENS /
          CONSOLE_EVAL_TOKENS (scopes admin > write > read > eval).
  key     Create, list and revoke stored API keys (run "console auth key help").
          Needs a backend with a key store (SQLite); the CLI and the server
          must share the same database file for revocation to reach a running
          server.
  audit   List and prune the key audit trail (run "console auth audit help").

Configuration:
  CONSOLE_<SCOPE>_TOKENS       Comma-separated tokens. Split on commas only; each
                               entry is trimmed of surrounding whitespace and
                               empty entries are skipped. Tokens must not
                               contain commas or leading/trailing whitespace.
  CONSOLE_<SCOPE>_TOKENS_FILE  Read tokens from a file instead: one per line,
                               each line trimmed, blank lines and lines starting
                               with # ignored. Setting both the variable and its
                               _FILE is an error.
  CONSOLE_AUTH                 auto (default): auth is on when any token is set.
                               disabled: never authenticate (only for use behind
                               an authenticating proxy).

console serve refuses to start on a non-loopback address (including :8080 and
0.0.0.0) unless auth is enabled or CONSOLE_AUTH=disabled.
`

func (c *cli) cmdAuth(args []string, cfg config.Config) error {
	ctx, cancel := signalContext()
	defer cancel()
	var a *app.App
	defer func() {
		if a != nil {
			a.Close()
		}
	}()
	env := authEnv{
		RetentionDays: cfg.AuditRetentionDays,
		Err:           os.Stderr,
		Keys: func() (auth.KeyStore, error) {
			var err error
			if a, err = app.New(ctx, cfg, c.appOpts()...); err != nil {
				return nil, err
			}
			return a.Keys, nil
		},
	}
	return runAuth(ctx, args, os.Stdout, env)
}

func runAuth(ctx context.Context, args []string, out io.Writer, env authEnv) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(out, authUsage)
		return nil
	}
	switch args[0] {
	case "token":
		if len(args) > 1 {
			return fmt.Errorf("auth token takes no arguments")
		}
		tok, err := auth.GenerateToken()
		if err != nil {
			return err
		}
		fmt.Fprintln(out, tok)
		return nil
	case "key":
		return runAuthKey(ctx, args[1:], out, env)
	case "audit":
		return runAuthAudit(ctx, args[1:], out, env)
	default:
		return usagef("unknown auth subcommand %q (run \"console auth help\")", args[0])
	}
}
