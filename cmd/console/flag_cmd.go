package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/core"
)

const flagUsage = `Manage feature flags.

Usage:
  console flag list
  console flag get <key>
  console flag create <key> [-desc ...] [-scope all|beta|alpha|cohort|experiment] [-rollout 0-100] [-enabled]
  console flag enable <key>
  console flag disable <key>
  console flag delete <key>
  console flag eval <key> -subject <id> [-attr k=v ...]
`

func cmdFlag(args []string, cfg config.Config) error {
	if len(args) == 0 {
		fmt.Print(flagUsage)
		return nil
	}
	sub, rest := args[0], args[1:]

	ctx, cancel := signalContext()
	defer cancel()
	a, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer a.Close()

	switch sub {
	case "list", "ls":
		return flagList(ctx, a)
	case "get":
		if len(rest) != 1 {
			return fmt.Errorf("usage: console flag get <key>")
		}
		f, err := a.Flags.Get(ctx, rest[0])
		if err != nil {
			return err
		}
		printFlag(f)
		return nil
	case "create":
		return flagCreate(ctx, a, rest)
	case "enable":
		return flagSetEnabled(ctx, a, rest, true)
	case "disable":
		return flagSetEnabled(ctx, a, rest, false)
	case "delete", "rm":
		if len(rest) != 1 {
			return fmt.Errorf("usage: console flag delete <key>")
		}
		if err := a.Flags.Delete(ctx, rest[0]); err != nil {
			return err
		}
		fmt.Printf("deleted flag %q\n", rest[0])
		return nil
	case "eval":
		return flagEval(ctx, a, rest)
	default:
		return fmt.Errorf("unknown flag subcommand %q", sub)
	}
}

func flagList(ctx context.Context, a *app.App) error {
	fs, err := a.Flags.List(ctx)
	if err != nil {
		return err
	}
	if len(fs) == 0 {
		fmt.Println("no flags yet — create one with: console flag create <key>")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tENABLED\tSCOPE\tROLLOUT\tDESCRIPTION")
	for _, f := range fs {
		fmt.Fprintf(w, "%s\t%t\t%s\t%d%%\t%s\n", f.Key, f.Enabled, f.Scope, f.Rollout, f.Description)
	}
	return w.Flush()
}

func flagCreate(ctx context.Context, a *app.App, args []string) error {
	key, rest := leadingArg(args)
	if key == "" {
		return fmt.Errorf("usage: console flag create <key> [flags]")
	}
	fs := flag.NewFlagSet("flag create", flag.ContinueOnError)
	desc := fs.String("desc", "", "description")
	scope := fs.String("scope", "all", "scope: all|beta|alpha|cohort|experiment")
	rollout := fs.Int("rollout", 0, "rollout percentage 0-100")
	enabled := fs.Bool("enabled", false, "enable immediately")
	cohort := fs.String("cohort", "", "cohort name (for -scope cohort)")
	exp := fs.String("experiment", "", "experiment name (for -scope experiment)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *rollout < 0 || *rollout > 100 {
		return fmt.Errorf("rollout must be 0-100, got %d", *rollout)
	}
	f := core.Flag{
		Key:         key,
		Description: *desc,
		Enabled:     *enabled,
		Scope:       core.Scope(*scope),
		Rollout:     *rollout,
		Cohort:      *cohort,
		Experiment:  *exp,
	}
	if err := a.Flags.Create(ctx, f); err != nil {
		return err
	}
	fmt.Printf("created flag %q (scope=%s rollout=%d%% enabled=%t)\n", f.Key, f.Scope, f.Rollout, f.Enabled)
	return nil
}

func flagSetEnabled(ctx context.Context, a *app.App, args []string, enabled bool) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: console flag enable|disable <key>")
	}
	f, err := a.Flags.Get(ctx, args[0])
	if err != nil {
		return err
	}
	f.Enabled = enabled
	if err := a.Flags.Update(ctx, f); err != nil {
		return err
	}
	fmt.Printf("flag %q enabled=%t\n", f.Key, enabled)
	return nil
}

func flagEval(ctx context.Context, a *app.App, args []string) error {
	key, rest := leadingArg(args)
	if key == "" {
		return fmt.Errorf("usage: console flag eval <key> -subject <id> [-attr k=v ...]")
	}
	fs := flag.NewFlagSet("flag eval", flag.ContinueOnError)
	subject := fs.String("subject", "", "subject id to evaluate for")
	var attrs attrFlags
	fs.Var(&attrs, "attr", "subject attribute k=v (repeatable)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	subj := core.Subject{Key: *subject, Attributes: attrs.m}
	ev, err := a.Flags.Evaluate(ctx, key, subj)
	if err != nil {
		return err
	}
	fmt.Printf("flag=%s subject=%s enabled=%t variant=%s value=%q reason=%s\n",
		ev.FlagKey, subj.Key, ev.Enabled, ev.Variant, ev.Value, ev.Reason)
	return nil
}

func printFlag(f core.Flag) {
	fmt.Printf("key:         %s\n", f.Key)
	fmt.Printf("description: %s\n", f.Description)
	fmt.Printf("enabled:     %t\n", f.Enabled)
	fmt.Printf("scope:       %s\n", f.Scope)
	fmt.Printf("rollout:     %d%%\n", f.Rollout)
	if f.Cohort != "" {
		fmt.Printf("cohort:      %s\n", f.Cohort)
	}
	if f.Experiment != "" {
		fmt.Printf("experiment:  %s\n", f.Experiment)
	}
	if len(f.Variants) > 0 {
		fmt.Println("variants:")
		for _, v := range f.Variants {
			fmt.Printf("  - %s = %q (weight %d)\n", v.Key, v.Value, v.Weight)
		}
	}
}

// attrFlags collects repeated -attr k=v flags into a map.
type attrFlags struct{ m map[string]string }

func (a *attrFlags) String() string { return fmt.Sprintf("%v", a.m) }

func (a *attrFlags) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok {
		return fmt.Errorf("attribute must be k=v, got %q", s)
	}
	if a.m == nil {
		a.m = map[string]string{}
	}
	a.m[k] = v
	return nil
}
