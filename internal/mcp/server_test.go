package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/core"
)

// newTestClient wires an MCP client to a server over in-memory transports and
// returns the connected client session. It backs the server with an in-memory
// SQLite Console (config.DB == "").
func newTestClient(t *testing.T, allowWrites bool) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()

	a, err := app.New(ctx, config.Config{DB: ""})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	srv := NewServer(NewEngineBackend(a), Options{Version: "test", AllowWrites: allowWrites})

	clientT, serverT := sdk.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// callText calls a tool and returns the first text-content block, failing the
// test if the call errored at the tool level.
func callText(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool %s returned tool error: %s", name, firstText(res.Content))
	}
	return firstText(res.Content)
}

func firstText(content []sdk.Content) string {
	for _, c := range content {
		if tc, ok := c.(*sdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func toolNames(t *testing.T, cs *sdk.ClientSession) map[string]bool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	return names
}

func TestReadOnlyServer_OmitsWriteTools(t *testing.T) {
	cs := newTestClient(t, false)
	names := toolNames(t, cs)

	for _, want := range []string{"list_flags", "evaluate_flag", "health_snapshot", "check_component"} {
		if !names[want] {
			t.Errorf("read tool %q missing", want)
		}
	}
	for _, unwanted := range []string{"create_flag", "set_flag_enabled", "delete_flag", "add_component", "delete_component"} {
		if names[unwanted] {
			t.Errorf("write tool %q exposed on a read-only server", unwanted)
		}
	}
}

func TestWriteServer_CreateAndEvaluate(t *testing.T) {
	cs := newTestClient(t, true)

	names := toolNames(t, cs)
	for _, want := range []string{"create_flag", "set_flag_enabled", "delete_flag", "add_component", "delete_component"} {
		if !names[want] {
			t.Fatalf("write tool %q missing on a -write server", want)
		}
	}

	// Create a fully-rolled-out flag, then evaluate it.
	callText(t, cs, "create_flag", map[string]any{
		"key":     "new-ui",
		"scope":   "all",
		"rollout": 100,
		"enabled": true,
	})

	// It should appear in list_flags.
	var flags []core.Flag
	if err := json.Unmarshal([]byte(callText(t, cs, "list_flags", nil)), &flags); err != nil {
		t.Fatalf("unmarshal list_flags: %v", err)
	}
	if len(flags) != 1 || flags[0].Key != "new-ui" {
		t.Fatalf("expected [new-ui], got %+v", flags)
	}

	// evaluate_flag at 100%% rollout is on for any subject.
	var ev core.Evaluation
	if err := json.Unmarshal([]byte(callText(t, cs, "evaluate_flag", map[string]any{
		"key":     "new-ui",
		"subject": "user-123",
	})), &ev); err != nil {
		t.Fatalf("unmarshal evaluate_flag: %v", err)
	}
	if ev.FlagKey != "new-ui" || !ev.Enabled {
		t.Errorf("evaluation = %+v, want enabled new-ui", ev)
	}

	// Disable it, then confirm the flag reads back disabled.
	callText(t, cs, "set_flag_enabled", map[string]any{"key": "new-ui", "enabled": false})
	var got core.Flag
	if err := json.Unmarshal([]byte(callText(t, cs, "get_flag", map[string]any{"key": "new-ui"})), &got); err != nil {
		t.Fatalf("unmarshal get_flag: %v", err)
	}
	if got.Enabled {
		t.Errorf("flag still enabled after set_flag_enabled=false")
	}
}

func TestHealthResource(t *testing.T) {
	cs := newTestClient(t, false)
	res, err := cs.ReadResource(context.Background(), &sdk.ReadResourceParams{URI: "console://health"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) == 0 || res.Contents[0].Text == "" {
		t.Fatal("empty health resource")
	}
	var hv healthView
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &hv); err != nil {
		t.Fatalf("unmarshal health resource: %v", err)
	}
	// No components yet → aggregate is unknown.
	if hv.State != "unknown" {
		t.Errorf("empty snapshot state = %q, want unknown", hv.State)
	}
}

func TestOnboardPrompt(t *testing.T) {
	cs := newTestClient(t, false)
	res, err := cs.GetPrompt(context.Background(), &sdk.GetPromptParams{
		Name:      "onboard",
		Arguments: map[string]string{"app": "Acme API", "description": "a REST API"},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(res.Messages) == 0 {
		t.Fatal("onboard prompt returned no messages")
	}
	tc, ok := res.Messages[0].Content.(*sdk.TextContent)
	if !ok || tc.Text == "" {
		t.Fatal("onboard prompt message has no text content")
	}
}
