// Package mcp exposes Console over the Model Context Protocol (MCP), so an AI
// agent (Claude Desktop, Claude Code, or any MCP client) can operate a Console
// instance as a set of tools: list and evaluate feature flags, read component
// health, and — when writes are enabled — create, toggle, and delete flags and
// components.
//
// MCP is a consumer surface, like the CLI and the dashboard — not one of
// Console's plugin seams (store/status/notify/llm). The server talks to Console
// through a Backend, which has two implementations: an in-process one over the
// flags/status engines (the default), and an HTTP one that targets a running
// `console serve` (selected with -addr). Handlers are written against Backend
// so they are identical in either mode.
package mcp

import (
	"context"

	"github.com/moosequest/console/internal/core"
)

// Backend is the set of Console operations the MCP tools need. Both the
// in-process engine backend and the HTTP-client backend satisfy it, so the tool
// handlers never depend on how Console is reached.
type Backend interface {
	// Flags.
	ListFlags(ctx context.Context) ([]core.Flag, error)
	GetFlag(ctx context.Context, key string) (core.Flag, error)
	CreateFlag(ctx context.Context, f core.Flag) error
	UpdateFlag(ctx context.Context, f core.Flag) error
	DeleteFlag(ctx context.Context, key string) error
	EvaluateFlag(ctx context.Context, key string, subj core.Subject) (core.Evaluation, error)

	// Components and health.
	ListComponents(ctx context.Context) ([]core.Component, error)
	GetComponent(ctx context.Context, key string) (core.Component, error)
	CreateComponent(ctx context.Context, c core.Component) error
	UpdateComponent(ctx context.Context, c core.Component) error
	DeleteComponent(ctx context.Context, key string) error
	CheckComponent(ctx context.Context, key string) (core.Check, error)
	HealthSnapshot(ctx context.Context) (core.Health, error)
}
