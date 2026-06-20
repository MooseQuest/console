package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/onboard"
)

const onboardUsage = `Onboard an application into Console.

Human mode (default) walks you through it interactively.
AI-Assisted mode drafts a plan from a one-line description (needs an LLM provider).

Usage:
  console onboard                                  # Human mode
  console onboard -ai -name <app> -desc "<what it is>"   # AI-Assisted mode
  console onboard ... -apply                       # apply without confirmation
  console onboard ... -guide setup.md              # write a prompt.ai-style guide
`

func cmdOnboard(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	useAI := fs.Bool("ai", false, "use AI-Assisted mode")
	name := fs.String("name", "", "app name (AI mode)")
	desc := fs.String("desc", "", "app description (AI mode)")
	apply := fs.Bool("apply", false, "apply the plan without confirmation")
	guidePath := fs.String("guide", "", "write a markdown setup guide to this path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()
	a, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer a.Close()

	var plan onboard.Plan
	if *useAI {
		if a.LLM == nil {
			return fmt.Errorf("AI-Assisted mode unavailable: no LLM plugin configured (set CONSOLE_LLM_PLUGIN to e.g. console-plugin-anthropic, with its key such as ANTHROPIC_API_KEY in the environment)")
		}
		if *name == "" || *desc == "" {
			return fmt.Errorf("AI mode needs -name and -desc")
		}
		fmt.Printf("drafting an onboarding plan for %q with %s…\n", *name, a.LLM.Name())
		plan, err = onboard.AI(ctx, a.LLM, *name, *desc)
	} else {
		plan, err = onboard.Human(ctx, os.Stdin, os.Stdout)
	}
	if err != nil {
		return err
	}

	printPlan(plan)

	if *guidePath != "" {
		if err := os.WriteFile(*guidePath, []byte(onboard.Guide(plan)), 0o644); err != nil {
			return fmt.Errorf("write guide: %w", err)
		}
		fmt.Printf("\nwrote setup guide to %s\n", *guidePath)
	}

	if !*apply {
		if !confirm("\nApply this plan now? [y/N] ") {
			fmt.Println("not applied. Re-run with -apply to apply non-interactively.")
			return nil
		}
	}
	n, err := onboard.Apply(ctx, a, plan)
	fmt.Printf("applied %d item(s)\n", n)
	return err
}

func printPlan(p onboard.Plan) {
	fmt.Printf("\nPlan for %s\n", p.App)
	if p.Description != "" {
		fmt.Printf("  %s\n", p.Description)
	}
	fmt.Printf("\n  Components (%d):\n", len(p.Components))
	for _, c := range p.Components {
		fmt.Printf("    - %s [%s] %s\n", c.Key, c.Provider, c.Config["url"])
	}
	fmt.Printf("  Flags (%d):\n", len(p.Flags))
	for _, f := range p.Flags {
		fmt.Printf("    - %s (scope=%s rollout=%d%% enabled=%t)\n", f.Key, f.Scope, f.Rollout, f.Enabled)
	}
	for _, n := range p.Notes {
		fmt.Printf("  note: %s\n", n)
	}
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	s := bufio.NewScanner(os.Stdin)
	if !s.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(s.Text()))
	return ans == "y" || ans == "yes"
}
