package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/moosequest/console/internal/auth"
)

const authUsage = `Manage API authentication.

Usage:
  console auth token

Subcommands:
  token   Print a freshly generated API token (csk_..., 256-bit) to stdout.
          Nothing is stored; place it in your secret store and configure it
          via CONSOLE_ADMIN_TOKENS / CONSOLE_WRITE_TOKENS / CONSOLE_READ_TOKENS /
          CONSOLE_EVAL_TOKENS (scopes admin > write > read > eval).

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

func cmdAuth(args []string) error { return runAuth(args, os.Stdout) }

func runAuth(args []string, out io.Writer) error {
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
	default:
		return fmt.Errorf("unknown auth subcommand %q (run \"console auth help\")", args[0])
	}
}
