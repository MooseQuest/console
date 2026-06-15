// Package status evaluates the health of monitored components. A Provider is a
// pluggable check (HTTP, TCP, a custom probe); the engine runs providers and
// aggregates their results into a core.Health snapshot.
package status

import (
	"context"

	"github.com/moosequest/console/internal/core"
)

// Provider performs a single health check for a component. Implementations are
// registered by name and selected per-component via core.Component.Provider.
type Provider interface {
	// Name identifies the provider, e.g. "http".
	Name() string
	// Check probes the component described by cfg and returns its state.
	Check(ctx context.Context, comp core.Component) core.Check
}
