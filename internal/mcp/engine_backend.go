package mcp

import (
	"context"
	"fmt"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/core"
)

// engineBackend is the default Backend: it drives the flags and status engines
// in-process, exactly as the CLI does. It needs no running server and works
// offline against the local store.
type engineBackend struct {
	app *app.App
}

// NewEngineBackend wraps an already-constructed App as a Backend. The caller
// retains ownership of the App (and its Close).
func NewEngineBackend(a *app.App) Backend { return &engineBackend{app: a} }

func (b *engineBackend) ListFlags(ctx context.Context) ([]core.Flag, error) {
	return b.app.Flags.List(ctx)
}

func (b *engineBackend) GetFlag(ctx context.Context, key string) (core.Flag, error) {
	return b.app.Flags.Get(ctx, key)
}

func (b *engineBackend) CreateFlag(ctx context.Context, f core.Flag) error {
	return b.app.Flags.Create(ctx, f)
}

func (b *engineBackend) UpdateFlag(ctx context.Context, f core.Flag) error {
	return b.app.Flags.Update(ctx, f)
}

func (b *engineBackend) DeleteFlag(ctx context.Context, key string) error {
	return b.app.Flags.Delete(ctx, key)
}

func (b *engineBackend) EvaluateFlag(ctx context.Context, key string, subj core.Subject) (core.Evaluation, error) {
	return b.app.Flags.Evaluate(ctx, key, subj)
}

func (b *engineBackend) ListComponents(ctx context.Context) ([]core.Component, error) {
	return b.app.Status.ListComponents(ctx)
}

func (b *engineBackend) GetComponent(ctx context.Context, key string) (core.Component, error) {
	return b.app.Status.GetComponent(ctx, key)
}

func (b *engineBackend) CreateComponent(ctx context.Context, c core.Component) error {
	return b.app.Status.CreateComponent(ctx, c)
}

func (b *engineBackend) UpdateComponent(ctx context.Context, c core.Component) error {
	return b.app.Status.UpdateComponent(ctx, c)
}

func (b *engineBackend) DeleteComponent(ctx context.Context, key string) error {
	return b.app.Status.DeleteComponent(ctx, key)
}

// CheckComponent looks up the component and runs its provider once, returning
// the fresh Check.
func (b *engineBackend) CheckComponent(ctx context.Context, key string) (core.Check, error) {
	comp, err := b.app.Status.GetComponent(ctx, key)
	if err != nil {
		return core.Check{}, fmt.Errorf("get component %q: %w", key, err)
	}
	return b.app.Status.Run(ctx, comp), nil
}

func (b *engineBackend) HealthSnapshot(ctx context.Context) (core.Health, error) {
	return b.app.Status.Snapshot(ctx)
}
