package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/core"
)

const statusUsage = `Manage status components and check health.

Usage:
  console status list
  console status add <key> -url <url> [-name ...] [-provider http]
  console status check [<key>]      # check one component, or all if omitted
  console status snapshot           # aggregate health across all components
  console status delete <key>
`

func cmdStatus(args []string, cfg config.Config) error {
	if len(args) == 0 {
		fmt.Print(statusUsage)
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
		return statusList(ctx, a)
	case "add":
		return statusAdd(ctx, a, rest)
	case "check":
		return statusCheck(ctx, a, rest)
	case "snapshot":
		return statusSnapshot(ctx, a)
	case "delete", "rm":
		if len(rest) != 1 {
			return fmt.Errorf("usage: console status delete <key>")
		}
		if err := a.Status.DeleteComponent(ctx, rest[0]); err != nil {
			return err
		}
		fmt.Printf("deleted component %q\n", rest[0])
		return nil
	default:
		return fmt.Errorf("unknown status subcommand %q", sub)
	}
}

func statusList(ctx context.Context, a *app.App) error {
	comps, err := a.Status.ListComponents(ctx)
	if err != nil {
		return err
	}
	if len(comps) == 0 {
		fmt.Println("no components yet — add one with: console status add <key> -url <url>")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tNAME\tPROVIDER\tTARGET")
	for _, c := range comps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Key, c.Name, c.Provider, c.Config["url"])
	}
	return w.Flush()
}

func statusAdd(ctx context.Context, a *app.App, args []string) error {
	key, rest := leadingArg(args)
	if key == "" {
		return fmt.Errorf("usage: console status add <key> -url <url>")
	}
	fs := flag.NewFlagSet("status add", flag.ContinueOnError)
	name := fs.String("name", "", "human-readable name")
	provider := fs.String("provider", "http", "status provider")
	url := fs.String("url", "", "URL to probe (for the http provider)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	c := core.Component{
		Key:      key,
		Name:     orDefault(*name, key),
		Provider: *provider,
		Config:   map[string]string{},
	}
	if *url != "" {
		c.Config["url"] = *url
	}
	if err := a.Status.CreateComponent(ctx, c); err != nil {
		return err
	}
	fmt.Printf("added component %q (provider=%s url=%s)\n", c.Key, c.Provider, *url)
	return nil
}

func statusCheck(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 1 {
		c, err := a.Status.GetComponent(ctx, args[0])
		if err != nil {
			return err
		}
		printCheck(a.Status.Run(ctx, c))
		return nil
	}
	checks, err := a.Status.RunAll(ctx)
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		fmt.Println("no components to check")
		return nil
	}
	for _, c := range checks {
		printCheck(c)
	}
	return nil
}

func statusSnapshot(ctx context.Context, a *app.App) error {
	h, err := a.Status.Snapshot(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("overall: %s (%d components, as of %s)\n",
		h.State, len(h.Components), h.CheckedAt.Format("15:04:05"))
	for _, c := range h.Components {
		printCheck(c)
	}
	return nil
}

func printCheck(c core.Check) {
	line := fmt.Sprintf("  %-12s %-12s", c.Component, c.State)
	if c.Latency > 0 {
		line += fmt.Sprintf(" %6dms", c.Latency.Milliseconds())
	}
	if c.Message != "" {
		line += "  " + c.Message
	}
	fmt.Println(line)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
