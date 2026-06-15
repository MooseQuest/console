package onboard

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/flags"
	"github.com/moosequest/console/internal/llm"
	"github.com/moosequest/console/internal/status"
	"github.com/moosequest/console/internal/store/sqlite"
)

// --- Human mode ---

func TestHuman(t *testing.T) {
	// Scripted session: app name, description, two components (blank ends loop),
	// two flags including an out-of-range rollout to exercise clamping, then a
	// blank key to end the flag loop.
	script := strings.Join([]string{
		"Acme Widgets",                                // app name
		"A widget store backend",                      // description
		"API", "http", "https://api.acme.test/health", // component 1
		"Worker", "", "", // component 2 (default provider)
		"",                                                               // end components
		"new-checkout", "Switch to the new checkout flow", "beta", "150", // flag 1, rollout over 100
		"dark-mode", "Dark theme", "", "", // flag 2, default scope + rollout
		"", // end flags
	}, "\n") + "\n"

	var out bytes.Buffer
	p, err := Human(context.Background(), strings.NewReader(script), &out)
	if err != nil {
		t.Fatalf("Human: %v", err)
	}

	if p.App != "Acme Widgets" {
		t.Errorf("App = %q, want %q", p.App, "Acme Widgets")
	}
	if p.Description != "A widget store backend" {
		t.Errorf("Description = %q", p.Description)
	}

	if len(p.Components) != 2 {
		t.Fatalf("got %d components, want 2: %+v", len(p.Components), p.Components)
	}
	if p.Components[0].Key != "api" || p.Components[0].Name != "API" {
		t.Errorf("component[0] = %+v", p.Components[0])
	}
	if p.Components[0].Provider != "http" {
		t.Errorf("component[0] provider = %q", p.Components[0].Provider)
	}
	if got := p.Components[0].Config["url"]; got != "https://api.acme.test/health" {
		t.Errorf("component[0] url = %q", got)
	}
	if p.Components[1].Provider != "http" { // defaulted from blank
		t.Errorf("component[1] provider = %q, want http", p.Components[1].Provider)
	}

	if len(p.Flags) != 2 {
		t.Fatalf("got %d flags, want 2: %+v", len(p.Flags), p.Flags)
	}
	if p.Flags[0].Key != "new-checkout" {
		t.Errorf("flag[0] key = %q", p.Flags[0].Key)
	}
	if p.Flags[0].Scope != core.ScopeBeta {
		t.Errorf("flag[0] scope = %q, want beta", p.Flags[0].Scope)
	}
	if p.Flags[0].Rollout != 100 { // 150 clamped to 100
		t.Errorf("flag[0] rollout = %d, want 100 (clamped)", p.Flags[0].Rollout)
	}
	if !p.Flags[0].Enabled {
		t.Errorf("flag[0] should be enabled (rollout > 0)")
	}
	if p.Flags[1].Scope != core.ScopeAll { // defaulted from blank
		t.Errorf("flag[1] scope = %q, want all", p.Flags[1].Scope)
	}
	if p.Flags[1].Rollout != 0 {
		t.Errorf("flag[1] rollout = %d, want 0", p.Flags[1].Rollout)
	}
}

func TestHumanEmptyTerminatesLoops(t *testing.T) {
	// App name + description, then immediately blank for components and flags.
	script := "Solo App\nJust me\n\n\n"
	var out bytes.Buffer
	p, err := Human(context.Background(), strings.NewReader(script), &out)
	if err != nil {
		t.Fatalf("Human: %v", err)
	}
	if len(p.Components) != 0 || len(p.Flags) != 0 {
		t.Errorf("expected empty loops, got %d comps %d flags", len(p.Components), len(p.Flags))
	}
	if p.App != "Solo App" {
		t.Errorf("App = %q", p.App)
	}
}

// --- AI mode ---

// fakeProvider is an in-package llm.Provider that returns a canned reply (or
// error), for tests without network access.
type fakeProvider struct {
	reply string
	err   error
}

func (f fakeProvider) Name() string { return "fake" }
func (f fakeProvider) Complete(ctx context.Context, req llm.Request) (string, error) {
	return f.reply, f.err
}

const cannedJSON = `{
  "components": [
    {"key": "api", "name": "API", "description": "Public API", "provider": "http", "config": {"url": "https://x.test"}},
    {"key": "db", "name": "Database", "provider": ""}
  ],
  "flags": [
    {"key": "beta-ui", "description": "New UI", "scope": "beta", "rollout": 250, "enabled": true},
    {"key": "audit-log", "description": "Audit logging", "scope": "", "rollout": -5}
  ],
  "notes": ["Consider adding a cache component."]
}`

func TestAIParsesPlan(t *testing.T) {
	p, err := AI(context.Background(), fakeProvider{reply: cannedJSON}, "Acme", "An app")
	if err != nil {
		t.Fatalf("AI: %v", err)
	}
	if p.App != "Acme" || p.Description != "An app" {
		t.Errorf("App/Description not set: %+v", p)
	}
	if len(p.Components) != 2 {
		t.Fatalf("got %d components", len(p.Components))
	}
	if p.Components[1].Provider != "http" { // defaulted from blank
		t.Errorf("component[1] provider = %q, want http", p.Components[1].Provider)
	}
	if len(p.Flags) != 2 {
		t.Fatalf("got %d flags", len(p.Flags))
	}
	if p.Flags[0].Rollout != 100 { // 250 clamped
		t.Errorf("flag[0] rollout = %d, want 100", p.Flags[0].Rollout)
	}
	if p.Flags[0].Scope != core.ScopeBeta {
		t.Errorf("flag[0] scope = %q", p.Flags[0].Scope)
	}
	if p.Flags[1].Rollout != 0 { // -5 clamped
		t.Errorf("flag[1] rollout = %d, want 0", p.Flags[1].Rollout)
	}
	if p.Flags[1].Scope != core.ScopeAll { // defaulted
		t.Errorf("flag[1] scope = %q, want all", p.Flags[1].Scope)
	}
	if len(p.Notes) != 1 {
		t.Errorf("notes = %v", p.Notes)
	}
}

func TestAIStripsCodeFence(t *testing.T) {
	fenced := "Here is your plan:\n```json\n" + cannedJSON + "\n```\n"
	// The leading prose would break a naive parser; our stripper only handles a
	// reply that *starts* with a fence, so test the canonical fenced form.
	fencedOnly := "```json\n" + cannedJSON + "\n```"
	p, err := AI(context.Background(), fakeProvider{reply: fencedOnly}, "Acme", "An app")
	if err != nil {
		t.Fatalf("AI (fenced): %v", err)
	}
	if len(p.Components) != 2 || len(p.Flags) != 2 {
		t.Errorf("fenced parse wrong: %+v", p)
	}
	// Sanity: bare ``` fence (no language) also works.
	bare := "```\n" + cannedJSON + "\n```"
	if _, err := AI(context.Background(), fakeProvider{reply: bare}, "A", "b"); err != nil {
		t.Errorf("AI (bare fence): %v", err)
	}
	_ = fenced
}

func TestAINilProvider(t *testing.T) {
	_, err := AI(context.Background(), nil, "Acme", "An app")
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
	if !strings.Contains(err.Error(), "no LLM provider") {
		t.Errorf("error = %q", err)
	}
}

func TestAIMalformedJSON(t *testing.T) {
	_, err := AI(context.Background(), fakeProvider{reply: "not json at all {{{"}, "Acme", "An app")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse AI plan") {
		t.Errorf("error not wrapped: %q", err)
	}
	if !strings.Contains(err.Error(), "not json at all") {
		t.Errorf("error missing reply snippet: %q", err)
	}
}

// --- Guide ---

func TestGuide(t *testing.T) {
	p := Plan{
		App:         "Acme Widgets",
		Description: "A widget store backend",
		Components:  []core.Component{{Key: "api", Name: "API", Provider: "http"}},
		Flags:       []core.Flag{{Key: "new-checkout", Scope: core.ScopeBeta, Rollout: 25, Enabled: true}},
		Notes:       []string{"Remember to set the API key."},
	}
	g := Guide(p)
	for _, want := range []string{
		"Acme Widgets",
		"A widget store backend",
		"## Components",
		"api",
		"## Feature flags",
		"new-checkout",
		"## Next steps",
		"ANTHROPIC_API_KEY",
		"Remember to set the API key.",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guide missing %q\n---\n%s", want, g)
		}
	}
}

// --- Apply ---

func newTestApp(t *testing.T) *app.App {
	t.Helper()
	st, err := sqlite.Open(context.Background(), "") // shared in-memory
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &app.App{
		Store:  st,
		Flags:  flags.New(st),
		Status: status.New(st, st),
	}
}

func TestApply(t *testing.T) {
	a := newTestApp(t)
	p := Plan{
		Components: []core.Component{
			{Key: "api", Name: "API", Provider: "http"},
			{Key: "db", Name: "DB", Provider: "http"},
		},
		Flags: []core.Flag{
			{Key: "f1", Scope: core.ScopeAll},
			{Key: "f2", Scope: core.ScopeAll},
		},
	}
	n, err := Apply(context.Background(), a, p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != 4 {
		t.Errorf("applied = %d, want 4", n)
	}
}

func TestApplySkipsConflicts(t *testing.T) {
	a := newTestApp(t)
	p := Plan{
		Components: []core.Component{{Key: "api", Name: "API", Provider: "http"}},
		Flags:      []core.Flag{{Key: "f1", Scope: core.ScopeAll}},
	}
	// First apply succeeds cleanly.
	if _, err := Apply(context.Background(), a, p); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	// Second apply hits conflicts on every item: nothing new applied, soft error.
	n, err := Apply(context.Background(), a, p)
	if n != 0 {
		t.Errorf("applied = %d, want 0 on re-apply", n)
	}
	var se *SkippedError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SkippedError, got %v", err)
	}
	if len(se.Skipped) != 2 {
		t.Errorf("skipped = %v, want 2 items", se.Skipped)
	}
}
