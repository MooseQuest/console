package status

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/store"
)

// Engine runs status providers against components and aggregates the results.
// It depends only on the narrow ComponentStore and CheckStore seams so it can
// be wired against any backend (or a fake in tests).
type Engine struct {
	comps  store.ComponentStore
	checks store.CheckStore

	mu        sync.RWMutex
	providers map[string]Provider

	// emit, when set, receives an Event on a meaningful health transition. It is
	// optional: nil disables emission (and the extra read it requires).
	emit func(core.Event)
}

// SetEmitter installs an event sink (e.g. a notify.Dispatcher's Emit). Passing
// nil disables emission. Safe to call once at startup before checks run.
func (e *Engine) SetEmitter(fn func(core.Event)) { e.emit = fn }

// New builds an Engine and registers the given providers by Name.
func New(comps store.ComponentStore, checks store.CheckStore, providers ...Provider) *Engine {
	e := &Engine{
		comps:     comps,
		checks:    checks,
		providers: make(map[string]Provider, len(providers)),
	}
	for _, p := range providers {
		e.Register(p)
	}
	return e
}

// Register adds (or replaces) a provider, keyed by its Name.
func (e *Engine) Register(p Provider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providers[p.Name()] = p
}

// provider returns the named provider, if registered.
func (e *Engine) provider(name string) (Provider, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.providers[name]
	return p, ok
}

// Component CRUD passthroughs to the ComponentStore.

// CreateComponent persists a new component.
func (e *Engine) CreateComponent(ctx context.Context, c core.Component) error {
	return e.comps.CreateComponent(ctx, c)
}

// GetComponent returns a component by key, or core.ErrNotFound.
func (e *Engine) GetComponent(ctx context.Context, key string) (core.Component, error) {
	return e.comps.GetComponent(ctx, key)
}

// ListComponents returns all components.
func (e *Engine) ListComponents(ctx context.Context) ([]core.Component, error) {
	return e.comps.ListComponents(ctx)
}

// UpdateComponent persists changes to an existing component.
func (e *Engine) UpdateComponent(ctx context.Context, c core.Component) error {
	return e.comps.UpdateComponent(ctx, c)
}

// DeleteComponent removes a component by key.
func (e *Engine) DeleteComponent(ctx context.Context, key string) error {
	return e.comps.DeleteComponent(ctx, key)
}

// Run checks a single component with its configured provider, records the
// result, and returns it. If the provider is unknown the result is a
// StateUnknown check; it is still recorded so the snapshot reflects reality.
func (e *Engine) Run(ctx context.Context, comp core.Component) core.Check {
	// Capture the prior state before recording, but only when an emitter is
	// installed — otherwise skip the extra read.
	prevState := core.StateUnknown
	if e.emit != nil {
		if prev, err := e.checks.LatestCheck(ctx, comp.Key); err == nil {
			prevState = prev.State
		}
	}

	var check core.Check
	if p, ok := e.provider(comp.Provider); ok {
		check = p.Check(ctx, comp)
		if check.Component == "" {
			check.Component = comp.Key
		}
	} else {
		check = core.Check{
			Component: comp.Key,
			State:     core.StateUnknown,
			Message:   fmt.Sprintf("unknown provider %q", comp.Provider),
			CheckedAt: time.Now().UTC(),
		}
	}

	_ = e.checks.RecordCheck(ctx, check)

	if e.emit != nil {
		if et, ok := classifyTransition(prevState, check.State); ok {
			e.emit(transitionEvent(et, comp, prevState, check))
		}
	}
	return check
}

// classifyTransition maps a state change to an event type, returning false when
// the change is not noteworthy. A first-ever check is modeled as a transition
// from StateUnknown, so a newly-added broken component still alerts, while a
// healthy first check (unknown -> operational) stays quiet.
func classifyTransition(prev, cur core.HealthState) (core.EventType, bool) {
	if cur == prev {
		return "", false
	}
	switch cur {
	case core.StateDown:
		return core.EventComponentDown, true
	case core.StateDegraded:
		return core.EventComponentDegraded, true
	case core.StateOperational:
		if prev == core.StateDown || prev == core.StateDegraded {
			return core.EventComponentRecovered, true
		}
		return "", false
	default: // -> unknown is not noteworthy
		return "", false
	}
}

// transitionEvent builds the Event for a component health transition.
func transitionEvent(et core.EventType, comp core.Component, prev core.HealthState, check core.Check) core.Event {
	name := comp.Name
	if name == "" {
		name = comp.Key
	}
	title := fmt.Sprintf("%s is %s", name, check.State)
	if et == core.EventComponentRecovered {
		title = fmt.Sprintf("%s recovered (now %s)", name, check.State)
	}
	return core.Event{
		Type:      et,
		Title:     title,
		Message:   check.Message,
		Component: comp.Key,
		From:      prev,
		To:        check.State,
		At:        check.CheckedAt,
	}
}

// RunAll checks every component and returns the resulting checks. Components
// are probed concurrently; an error is returned only if listing fails.
func (e *Engine) RunAll(ctx context.Context) ([]core.Check, error) {
	comps, err := e.comps.ListComponents(ctx)
	if err != nil {
		return nil, err
	}

	checks := make([]core.Check, len(comps))
	var wg sync.WaitGroup
	for i, comp := range comps {
		wg.Add(1)
		go func(i int, comp core.Component) {
			defer wg.Done()
			checks[i] = e.Run(ctx, comp)
		}(i, comp)
	}
	wg.Wait()
	return checks, nil
}

// Snapshot reads the latest check per component and aggregates them into a
// core.Health.
//
// Aggregation rule: the overall State is the worst (highest) state across
// components, but StateUnknown is treated as least-severe so a single
// not-yet-checked component cannot mask a real StateDown. When there are no
// checks at all, the overall State is StateUnknown.
func (e *Engine) Snapshot(ctx context.Context) (core.Health, error) {
	checks, err := e.checks.LatestChecks(ctx)
	if err != nil {
		return core.Health{}, err
	}

	health := core.Health{
		Components: checks,
		CheckedAt:  time.Now().UTC(),
	}

	if len(checks) == 0 {
		health.State = core.StateUnknown
		return health, nil
	}

	// severity orders Unknown below Operational so it never dominates; the
	// other states keep their natural ordering.
	severity := func(s core.HealthState) int {
		switch s {
		case core.StateUnknown:
			return -1
		default:
			return int(s)
		}
	}

	worst := checks[0].State
	for _, c := range checks[1:] {
		if severity(c.State) > severity(worst) {
			worst = c.State
		}
	}
	health.State = worst
	return health, nil
}
