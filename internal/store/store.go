// Package store defines the persistence seam for Console. Store is the plugin
// interface; concrete backends (SQLite by default, Postgres later) live in
// subpackages and are selected at startup. Nothing above this package knows
// which backend is in use.
package store

import (
	"context"

	"github.com/moosequest/console/internal/core"
)

// Store persists Console's domain objects. Implementations must be safe for
// concurrent use. All lookups return core.ErrNotFound when the key is absent;
// creates return core.ErrConflict when the key already exists.
type Store interface {
	FlagStore
	ComponentStore
	CheckStore

	// Ping verifies the backend is reachable.
	Ping(ctx context.Context) error
	// Close releases any held resources.
	Close() error
}

// FlagStore persists feature flags.
type FlagStore interface {
	CreateFlag(ctx context.Context, f core.Flag) error
	GetFlag(ctx context.Context, key string) (core.Flag, error)
	ListFlags(ctx context.Context) ([]core.Flag, error)
	UpdateFlag(ctx context.Context, f core.Flag) error
	DeleteFlag(ctx context.Context, key string) error
}

// ComponentStore persists monitored components.
type ComponentStore interface {
	CreateComponent(ctx context.Context, c core.Component) error
	GetComponent(ctx context.Context, key string) (core.Component, error)
	ListComponents(ctx context.Context) ([]core.Component, error)
	UpdateComponent(ctx context.Context, c core.Component) error
	DeleteComponent(ctx context.Context, key string) error
}

// CheckStore persists health-check observations. RecordCheck appends; the
// Latest* methods read back the most recent state.
type CheckStore interface {
	RecordCheck(ctx context.Context, c core.Check) error
	LatestCheck(ctx context.Context, component string) (core.Check, error)
	LatestChecks(ctx context.Context) ([]core.Check, error)
}
