// Package app is Console's composition root. It wires the configured storage
// backend, the flag and status engines, and the LLM provider into a single App
// value that the server, CLI, and onboarding flows depend on. Nothing here
// contains business logic — it only assembles the pieces.
package app

import (
	"context"
	"fmt"

	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/flags"
	"github.com/moosequest/console/internal/llm"
	"github.com/moosequest/console/internal/notify"
	"github.com/moosequest/console/internal/notify/slack"
	"github.com/moosequest/console/internal/plugin"
	"github.com/moosequest/console/internal/status"
	"github.com/moosequest/console/internal/status/cloudflare"
	"github.com/moosequest/console/internal/store"
	"github.com/moosequest/console/internal/store/sqlite"
)

// App holds the assembled, ready-to-use Console subsystems.
type App struct {
	Config config.Config
	Store  store.Store
	Flags  *flags.Engine
	Status *status.Engine
	// LLM is the AI-Assisted onboarding provider. It is nil when no provider is
	// configured; callers must treat AI-Assisted mode as unavailable in that
	// case and fall back to Human mode.
	LLM llm.Provider
	// Notify fans status transitions and flag changes out to registered sinks
	// (e.g. Slack). Always non-nil; it may simply have no notifiers registered.
	Notify *notify.Dispatcher
}

// New assembles an App from cfg. It opens the storage backend, constructs the
// flag and status engines (registering the built-in HTTP status provider), and
// selects the LLM provider when one is configured. The caller owns Close.
func New(ctx context.Context, cfg config.Config) (*App, error) {
	st, err := openStore(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	a := &App{
		Config: cfg,
		Store:  st,
		Flags:  flags.New(st),
		Status: status.New(st, st,
			&status.HTTPProvider{},
			cloudflare.New(cloudflare.WithToken(cfg.CloudflareToken)),
		),
		LLM:    newLLM(cfg),
		Notify: newNotify(cfg),
	}

	// Wire the engines to the dispatcher only when something is listening, so
	// emission (and the extra read it costs) is skipped when no sink is set up.
	if a.Notify.Len() > 0 {
		a.Status.SetEmitter(a.Notify.Emit)
		a.Flags.SetEmitter(a.Notify.Emit)
	}
	return a, nil
}

// openStore opens the storage backend. When cfg.StorePlugin is set it launches
// that out-of-process plugin (e.g. Postgres) and uses it over gRPC; otherwise it
// uses the built-in SQLite backend. The core does not import any plugin-only
// backend — those live in their own plugin executables.
func openStore(ctx context.Context, cfg config.Config) (store.Store, error) {
	if cfg.StorePlugin != "" {
		return plugin.LoadStore(cfg.StorePlugin)
	}
	return sqlite.Open(ctx, cfg.DB)
}

// newNotify builds the notification dispatcher, registering sinks that are
// configured. An empty dispatcher (no sinks) is returned otherwise.
func newNotify(cfg config.Config) *notify.Dispatcher {
	d := notify.NewDispatcher()
	if cfg.SlackWebhookURL != "" {
		d.Register(slack.New(cfg.SlackWebhookURL))
	}
	return d
}

// newLLM builds the configured LLM provider, or returns nil to disable
// AI-Assisted mode. Unknown provider names also yield nil.
func newLLM(cfg config.Config) llm.Provider {
	switch cfg.LLMProvider {
	case "anthropic":
		var opts []llm.Option
		if cfg.Model != "" {
			opts = append(opts, llm.WithModel(cfg.Model))
		}
		return llm.NewAnthropic(cfg.AnthropicKey, opts...)
	default:
		return nil
	}
}

// Close releases the App's resources (currently the store).
func (a *App) Close() error {
	if a.Store != nil {
		return a.Store.Close()
	}
	return nil
}
