package onboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/llm"
)

// aiSystemPrompt instructs the model to act as a Console setup assistant and to
// reply with ONLY a JSON object matching the schema below — no prose, no
// explanation. The schema is documented inline so the model has a single source
// of truth for the shape we will unmarshal.
const aiSystemPrompt = `You are Console's onboarding assistant. Console is a feature-flag and status-monitoring tool. Given an application's name and description, propose which components to monitor and which feature flags to create.

Respond with ONLY a single JSON object — no markdown, no code fences, no commentary — matching exactly this schema:

{
  "components": [
    {"key": "string slug", "name": "display name", "description": "what it is", "provider": "http", "config": {"url": "https://..."}}
  ],
  "flags": [
    {"key": "string", "description": "what it gates", "scope": "all|beta|alpha|cohort|experiment", "rollout": 0, "enabled": false}
  ],
  "notes": ["short advisory strings"]
}

Rules: provider defaults to "http"; scope defaults to "all"; rollout is an integer 0..100. Keep keys lowercase and hyphen-separated. Suggest 2-5 components and 2-5 flags that fit the description.`

// aiPlan mirrors the JSON contract in aiSystemPrompt. It is decoded from the
// model's reply and then mapped + normalized into a Plan.
type aiPlan struct {
	Components []struct {
		Key         string            `json:"key"`
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Provider    string            `json:"provider"`
		Config      map[string]string `json:"config"`
	} `json:"components"`
	Flags []struct {
		Key         string `json:"key"`
		Description string `json:"description"`
		Scope       string `json:"scope"`
		Rollout     int    `json:"rollout"`
		Enabled     bool   `json:"enabled"`
	} `json:"flags"`
	Notes []string `json:"notes"`
}

// AI drafts a Plan from a free-text description by asking the LLM provider p to
// fill out the documented JSON schema. It returns a clear error when no provider
// is configured (p == nil), so callers can fall back to Human mode. The model's
// reply is parsed robustly — a wrapping ```json code fence is stripped — and the
// decoded plan is normalized (rollout clamped 0..100, scope defaulted to "all",
// provider defaulted to "http", component keys minted from names when absent).
// On unparseable output the returned error includes a snippet of the reply.
func AI(ctx context.Context, p llm.Provider, appName, description string) (Plan, error) {
	if p == nil {
		return Plan{}, errors.New("AI-Assisted mode unavailable: no LLM provider configured")
	}

	req := llm.Request{
		System: aiSystemPrompt,
		Messages: []llm.Message{{
			Role: llm.RoleUser,
			Text: fmt.Sprintf("App name: %s\n\nDescription:\n%s", appName, description),
		}},
		MaxTokens: 2048,
	}

	reply, err := p.Complete(ctx, req)
	if err != nil {
		return Plan{}, fmt.Errorf("llm complete: %w", err)
	}

	raw := stripCodeFence(reply)

	var ap aiPlan
	if err := json.Unmarshal([]byte(raw), &ap); err != nil {
		return Plan{}, fmt.Errorf("parse AI plan: %w (reply snippet: %s)", err, snippet(reply))
	}

	plan := Plan{App: appName, Description: description, Notes: ap.Notes}

	for _, c := range ap.Components {
		key := strings.TrimSpace(c.Key)
		if key == "" {
			key = slug(c.Name)
		}
		plan.Components = append(plan.Components, core.Component{
			Key:         key,
			Name:        strings.TrimSpace(c.Name),
			Description: strings.TrimSpace(c.Description),
			Provider:    defaultProvider(c.Provider),
			Config:      c.Config,
		})
	}

	for _, f := range ap.Flags {
		plan.Flags = append(plan.Flags, core.Flag{
			Key:         strings.TrimSpace(f.Key),
			Description: strings.TrimSpace(f.Description),
			Scope:       normalizeScope(f.Scope),
			Rollout:     clampRollout(f.Rollout),
			Enabled:     f.Enabled,
		})
	}

	return plan, nil
}

// stripCodeFence removes a single wrapping Markdown code fence from s, if present
// — handling both ```json ... ``` and bare ``` ... ``` forms. When no fence is
// found the trimmed input is returned unchanged, so plain-JSON replies pass
// through untouched.
func stripCodeFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	// Drop the opening fence line (```, ```json, ```JSON, etc.).
	if nl := strings.IndexByte(t, '\n'); nl >= 0 {
		t = t[nl+1:]
	} else {
		t = strings.TrimPrefix(t, "```")
	}
	// Drop a trailing closing fence.
	if idx := strings.LastIndex(t, "```"); idx >= 0 {
		t = t[:idx]
	}
	return strings.TrimSpace(t)
}

// snippet returns the first 200 runes of s, suitable for embedding in an error.
func snippet(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}
