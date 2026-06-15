package onboard

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/moosequest/console/internal/core"
)

// Human runs the guided, interactive onboarding wizard. It reads answers from in
// and writes prompts to out, so it is fully testable with a strings.NewReader
// session and a bytes.Buffer — it never touches os.Stdin directly.
//
// The flow is: app name, app description, then two repeating loops. The component
// loop asks for a name (blank ends the loop), a provider (default "http"), and a
// url/config value. The flag loop asks for a key (blank ends the loop), a
// description, a scope (default "all"), and a rollout percentage (default 0,
// clamped to 0..100). Input is trimmed throughout. The assembled Plan is
// returned; ctx is honoured between prompts so a cancelled wizard stops promptly.
func Human(ctx context.Context, in io.Reader, out io.Writer) (Plan, error) {
	sc := bufio.NewScanner(in)
	// Allow long pasted config values without tripping the default token cap.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	ask := func(prompt string) (string, bool) {
		fmt.Fprint(out, prompt)
		if !sc.Scan() {
			return "", false
		}
		return strings.TrimSpace(sc.Text()), true
	}

	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}

	fmt.Fprintln(out, "Console onboarding — let's register your application.")

	name, _ := ask("App name: ")
	desc, _ := ask("App description: ")

	p := Plan{App: name, Description: desc}

	// Component loop.
	fmt.Fprintln(out, "\nAdd components to monitor (blank name to finish).")
	for {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		cname, ok := ask("  Component name (blank to stop): ")
		if !ok || cname == "" {
			break
		}
		provider, _ := ask("  Provider [http]: ")
		cfg, _ := ask("  URL / config value: ")

		comp := core.Component{
			Key:      slug(cname),
			Name:     cname,
			Provider: defaultProvider(provider),
		}
		if cfg != "" {
			comp.Config = map[string]string{"url": cfg}
		}
		p.Components = append(p.Components, comp)
	}

	// Flag loop.
	fmt.Fprintln(out, "\nAdd feature flags (blank key to finish).")
	for {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		key, ok := ask("  Flag key (blank to stop): ")
		if !ok || key == "" {
			break
		}
		fdesc, _ := ask("  Description: ")
		scope, _ := ask("  Scope (all/beta/alpha/cohort/experiment) [all]: ")
		rolloutStr, _ := ask("  Rollout %% [0]: ")

		rollout := 0
		if rolloutStr != "" {
			if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(rolloutStr), "%")); err == nil {
				rollout = clampRollout(n)
			} else {
				fmt.Fprintf(out, "  (couldn't parse %q as a number — defaulting rollout to 0)\n", rolloutStr)
			}
		}

		p.Flags = append(p.Flags, core.Flag{
			Key:         key,
			Description: fdesc,
			Scope:       normalizeScope(scope),
			Rollout:     rollout,
			Enabled:     rollout > 0,
		})
	}

	fmt.Fprintf(out, "\nDrafted a plan with %d component(s) and %d flag(s).\n", len(p.Components), len(p.Flags))
	return p, nil
}
