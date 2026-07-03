package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moosequest/console/internal/core"
)

// Options configures an MCP server.
type Options struct {
	// Name and Version identify the server to the MCP client.
	Name, Version string
	// AllowWrites registers the mutating tools (create/toggle/delete flags and
	// components). It is off by default: without it the server exposes only
	// read and flag-evaluation tools. Console has no built-in auth, so writes
	// are opt-in.
	AllowWrites bool
}

// NewServer builds an MCP server over the given Backend. Read and
// flag-evaluation tools are always registered; mutating tools are registered
// only when opts.AllowWrites is true. Two resources (console://health,
// console://flags) and an "onboard" prompt are always available.
func NewServer(b Backend, opts Options) *mcp.Server {
	if opts.Name == "" {
		opts.Name = "console"
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: opts.Name, Version: opts.Version}, nil)

	registerReadTools(srv, b)
	if opts.AllowWrites {
		registerWriteTools(srv, b)
	}
	registerResources(srv, b)
	registerPrompts(srv)
	return srv
}

// Run builds the server and serves it over stdio until ctx is cancelled or the
// client disconnects.
func Run(ctx context.Context, b Backend, opts Options) error {
	return NewServer(b, opts).Run(ctx, &mcp.StdioTransport{})
}

// --- helpers ---

func boolPtr(b bool) *bool { return &b }

// jsonResult renders v as indented JSON in a single text content block. Console
// domain types marshal to their documented JSON shape, so the agent receives
// the same structure the HTTP API returns.
func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal result: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(buf)}}}, nil, nil
}

// checkView renders a core.Check with a human-readable state string and
// milliseconds, since core.HealthState marshals as an opaque integer.
type checkView struct {
	Component string    `json:"component"`
	State     string    `json:"state"`
	Message   string    `json:"message,omitempty"`
	LatencyMS int64     `json:"latency_ms,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

func viewCheck(c core.Check) checkView {
	return checkView{
		Component: c.Component,
		State:     c.State.String(),
		Message:   c.Message,
		LatencyMS: c.Latency.Milliseconds(),
		CheckedAt: c.CheckedAt,
	}
}

type healthView struct {
	State      string      `json:"state"`
	Components []checkView `json:"components"`
	CheckedAt  time.Time   `json:"checked_at"`
}

func viewHealth(h core.Health) healthView {
	v := healthView{State: h.State.String(), CheckedAt: h.CheckedAt}
	for _, c := range h.Components {
		v.Components = append(v.Components, viewCheck(c))
	}
	return v
}

// --- tool input types (schemas inferred from these via jsonschema tags) ---

type emptyInput struct{}

type flagKeyInput struct {
	Key string `json:"key" jsonschema:"the flag key"`
}

type componentKeyInput struct {
	Key string `json:"key" jsonschema:"the component key"`
}

type evaluateInput struct {
	Key        string            `json:"key" jsonschema:"the flag key to evaluate"`
	Subject    string            `json:"subject" jsonschema:"the subject id to evaluate for (e.g. a user id)"`
	Attributes map[string]string `json:"attributes,omitempty" jsonschema:"optional subject attributes used by scope and cohort matching (e.g. audience=beta)"`
}

type createFlagInput struct {
	Key         string `json:"key" jsonschema:"the flag key (stable identifier)"`
	Description string `json:"description,omitempty" jsonschema:"human-readable description"`
	Scope       string `json:"scope,omitempty" jsonschema:"audience scope: all, beta, alpha, cohort, or experiment (default all)"`
	Rollout     int    `json:"rollout,omitempty" jsonschema:"percentage of in-scope subjects who get the on variant, 0-100"`
	Enabled     bool   `json:"enabled,omitempty" jsonschema:"whether the flag is enabled on creation"`
	Cohort      string `json:"cohort,omitempty" jsonschema:"cohort name (when scope is cohort)"`
	Experiment  string `json:"experiment,omitempty" jsonschema:"experiment name (when scope is experiment)"`
}

type setEnabledInput struct {
	Key     string `json:"key" jsonschema:"the flag key"`
	Enabled bool   `json:"enabled" jsonschema:"true to enable the flag, false to disable it"`
}

type addComponentInput struct {
	Key         string            `json:"key" jsonschema:"the component key (stable identifier)"`
	Name        string            `json:"name,omitempty" jsonschema:"display name"`
	Description string            `json:"description,omitempty" jsonschema:"human-readable description"`
	Provider    string            `json:"provider" jsonschema:"the status provider that checks this component (e.g. http)"`
	Config      map[string]string `json:"config,omitempty" jsonschema:"provider config, e.g. url=https://example.com/health for the http provider"`
}

// --- read tools ---

func registerReadTools(s *mcp.Server, b Backend) {
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_flags",
		Description: "List all feature flags with their scope, rollout, and enabled state.",
		Annotations: ro,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		fs, err := b.ListFlags(ctx)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(fs)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_flag",
		Description: "Get a single feature flag by key, including its variants.",
		Annotations: ro,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in flagKeyInput) (*mcp.CallToolResult, any, error) {
		f, err := b.GetFlag(ctx, in.Key)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(f)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "evaluate_flag",
		Description: "Evaluate a feature flag for a subject and return whether it is enabled, the served variant, and the reason. Evaluation is deterministic per (flag, subject).",
		Annotations: ro,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in evaluateInput) (*mcp.CallToolResult, any, error) {
		ev, err := b.EvaluateFlag(ctx, in.Key, core.Subject{Key: in.Subject, Attributes: in.Attributes})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(ev)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_components",
		Description: "List all monitored status components.",
		Annotations: ro,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		cs, err := b.ListComponents(ctx)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(cs)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_component",
		Description: "Get a single status component by key.",
		Annotations: ro,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in componentKeyInput) (*mcp.CallToolResult, any, error) {
		c, err := b.GetComponent(ctx, in.Key)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(c)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "health_snapshot",
		Description: "Get the aggregate health snapshot across all components (worst-wins). States are operational, degraded, down, or unknown.",
		Annotations: ro,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		h, err := b.HealthSnapshot(ctx)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(viewHealth(h))
	})

	// check_component runs a live probe; it is read-only from Console's data
	// model but reaches out to the target, so it is not marked idempotent.
	mcp.AddTool(s, &mcp.Tool{
		Name:        "check_component",
		Description: "Run a health check for one component now and return the fresh result.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in componentKeyInput) (*mcp.CallToolResult, any, error) {
		chk, err := b.CheckComponent(ctx, in.Key)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(viewCheck(chk))
	})
}

// --- write tools (opt-in) ---

func registerWriteTools(s *mcp.Server, b Backend) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_flag",
		Description: "Create a new feature flag.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createFlagInput) (*mcp.CallToolResult, any, error) {
		if in.Rollout < 0 || in.Rollout > 100 {
			return nil, nil, fmt.Errorf("rollout must be 0-100, got %d", in.Rollout)
		}
		scope := core.Scope(in.Scope)
		if scope == "" {
			scope = core.ScopeAll
		}
		f := core.Flag{
			Key:         in.Key,
			Description: in.Description,
			Enabled:     in.Enabled,
			Scope:       scope,
			Rollout:     in.Rollout,
			Cohort:      in.Cohort,
			Experiment:  in.Experiment,
		}
		if err := b.CreateFlag(ctx, f); err != nil {
			return nil, nil, err
		}
		return jsonResult(f)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_flag_enabled",
		Description: "Enable or disable an existing feature flag.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setEnabledInput) (*mcp.CallToolResult, any, error) {
		f, err := b.GetFlag(ctx, in.Key)
		if err != nil {
			return nil, nil, err
		}
		f.Enabled = in.Enabled
		if err := b.UpdateFlag(ctx, f); err != nil {
			return nil, nil, err
		}
		return jsonResult(f)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_flag",
		Description: "Delete a feature flag by key. This is destructive and cannot be undone.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in flagKeyInput) (*mcp.CallToolResult, any, error) {
		if err := b.DeleteFlag(ctx, in.Key); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]string{"deleted": in.Key})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_component",
		Description: "Add a status component to monitor.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addComponentInput) (*mcp.CallToolResult, any, error) {
		c := core.Component{
			Key:         in.Key,
			Name:        in.Name,
			Description: in.Description,
			Provider:    in.Provider,
			Config:      in.Config,
		}
		if err := b.CreateComponent(ctx, c); err != nil {
			return nil, nil, err
		}
		return jsonResult(c)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_component",
		Description: "Delete a status component by key. This is destructive and cannot be undone.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in componentKeyInput) (*mcp.CallToolResult, any, error) {
		if err := b.DeleteComponent(ctx, in.Key); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]string{"deleted": in.Key})
	})
}

// --- resources ---

func registerResources(s *mcp.Server, b Backend) {
	s.AddResource(&mcp.Resource{
		URI:         "console://health",
		Name:        "health",
		Description: "The current aggregate health snapshot across all components.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		h, err := b.HealthSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		return jsonResource(req.Params.URI, viewHealth(h))
	})

	s.AddResource(&mcp.Resource{
		URI:         "console://flags",
		Name:        "flags",
		Description: "The full feature-flag catalog.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		fs, err := b.ListFlags(ctx)
		if err != nil {
			return nil, err
		}
		return jsonResource(req.Params.URI, fs)
	})
}

func jsonResource(uri string, v any) (*mcp.ReadResourceResult, error) {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal resource: %w", err)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(buf)}},
	}, nil
}

// --- prompts ---

func registerPrompts(s *mcp.Server) {
	s.AddPrompt(&mcp.Prompt{
		Name:        "onboard",
		Description: "Guide setting up Console for an app: propose feature flags and status components, then create them with the write tools.",
		Arguments: []*mcp.PromptArgument{
			{Name: "app", Description: "the app or service name", Required: true},
			{Name: "description", Description: "what the app does", Required: false},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		app := req.Params.Arguments["app"]
		desc := req.Params.Arguments["description"]
		text := onboardPromptText(app, desc)
		return &mcp.GetPromptResult{
			Description: "Onboard " + app + " into Console",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: text},
			}},
		}, nil
	})
}

func onboardPromptText(app, desc string) string {
	if app == "" {
		app = "this app"
	}
	line := "I want to set up Console (feature flags + status monitoring) for " + app + "."
	if desc != "" {
		line += " It is: " + desc + "."
	}
	return line + `

Please help me onboard it:
1. Call list_flags and list_components to see what already exists.
2. Propose a small starter set of feature flags (with sensible scopes and rollouts) and status components (with the http provider and a health URL where you can infer one) for this app.
3. Show me the plan and, once I confirm, create them with create_flag and add_component. Use evaluate_flag to sanity-check a rollout for an example subject.

If the write tools are not available, output the plan as CLI commands (console flag create ..., console status add ...) instead.`
}
