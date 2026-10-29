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
          CONSOLE_EVAL_TOKENS.
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
