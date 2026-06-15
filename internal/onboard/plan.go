// Package onboard implements Console's onboarding subsystem: helping an operator
// register their application into Console — which components to monitor and which
// feature flags to create. It offers two modes that produce the same artifact:
// Human mode runs a guided, interactive terminal wizard; AI-Assisted mode asks an
// LLM to draft the plan from a free-text description. The resulting Plan can be
// applied to a live App (Apply) or rendered as a markdown setup guide (Guide).
package onboard

import (
	"fmt"
	"strings"

	"github.com/moosequest/console/internal/core"
)

// Plan is the shared output of both onboarding modes: a proposed Console setup
// for one application. It is a pure data value — building a Plan never touches
// storage. Apply turns it into real components and flags; Guide renders it as a
// human-readable setup guide.
type Plan struct {
	// App is the application's display name.
	App string
	// Description is a free-text summary of what the application does.
	Description string
	// Components are the parts of the app to monitor.
	Components []core.Component
	// Flags are the feature flags to create.
	Flags []core.Flag
	// Notes carry advisory messages — guidance produced while drafting the plan,
	// or skipped-item notices recorded during Apply.
	Notes []string
}

// normalizeScope returns a valid core.Scope, defaulting blank/unknown input to
// ScopeAll. It is case-insensitive and tolerant of surrounding whitespace.
func normalizeScope(s string) core.Scope {
	switch core.Scope(strings.ToLower(strings.TrimSpace(s))) {
	case core.ScopeBeta:
		return core.ScopeBeta
	case core.ScopeAlpha:
		return core.ScopeAlpha
	case core.ScopeCohort:
		return core.ScopeCohort
	case core.ScopeExperiment:
		return core.ScopeExperiment
	default:
		return core.ScopeAll
	}
}

// clampRollout coerces n into the valid rollout range 0..100.
func clampRollout(n int) int {
	switch {
	case n < 0:
		return 0
	case n > 100:
		return 100
	default:
		return n
	}
}

// defaultProvider returns p trimmed, falling back to "http" when blank.
func defaultProvider(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "http"
	}
	return p
}

// slug derives a stable, lowercase key from a free-text name: spaces and runs of
// non-alphanumeric characters collapse to single hyphens. It is used to mint a
// component Key when the source (a wizard answer) only supplied a display name.
func slug(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// Guide renders p as a prompt.ai-style markdown setup guide: a title, the app
// description, a Components section, a Feature flags section, any advisory notes,
// and a Next steps checklist. The output is deterministic given p.
func Guide(p Plan) string {
	var b strings.Builder

	title := strings.TrimSpace(p.App)
	if title == "" {
		title = "Your application"
	}
	fmt.Fprintf(&b, "# %s — Console setup guide\n\n", title)

	if d := strings.TrimSpace(p.Description); d != "" {
		b.WriteString(d)
		b.WriteString("\n\n")
	}

	b.WriteString("## Components\n\n")
	if len(p.Components) == 0 {
		b.WriteString("_No components to monitor yet._\n\n")
	} else {
		for _, c := range p.Components {
			name := c.Name
			if name == "" {
				name = c.Key
			}
			fmt.Fprintf(&b, "- **%s** (`%s`) — checked by the `%s` provider", name, c.Key, c.Provider)
			if c.Description != "" {
				fmt.Fprintf(&b, ": %s", c.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Feature flags\n\n")
	if len(p.Flags) == 0 {
		b.WriteString("_No feature flags to create yet._\n\n")
	} else {
		for _, f := range p.Flags {
			state := "disabled"
			if f.Enabled {
				state = "enabled"
			}
			fmt.Fprintf(&b, "- `%s` — %s, scope `%s`, %d%% rollout", f.Key, state, f.Scope, f.Rollout)
			if f.Description != "" {
				fmt.Fprintf(&b, " — %s", f.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(p.Notes) > 0 {
		b.WriteString("## Notes\n\n")
		for _, n := range p.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Next steps\n\n")
	b.WriteString("- [ ] set `ANTHROPIC_API_KEY` (and `CONSOLE_LLM_PROVIDER=anthropic`) to enable AI-Assisted onboarding\n")
	b.WriteString("- [ ] run `console serve` to start the dashboard and status checks\n")
	b.WriteString("- [ ] wire the Console SDK into your app to evaluate the flags above\n")
	b.WriteString("- [ ] confirm each component reports healthy on the status page\n")

	return b.String()
}
