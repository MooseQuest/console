// Package flags implements Console's feature-flag evaluation engine. It wraps a
// store.FlagStore for persistence and resolves a flag to an Evaluation for a
// given subject deterministically: the same (flag, subject) pair always yields
// the same result, so a rollout is stable across calls and processes.
package flags

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/obs"
	"github.com/moosequest/console/internal/store"
)

// Engine evaluates feature flags and provides CRUD passthrough to its store.
type Engine struct {
	store store.FlagStore

	// emit, when set, receives an Event when a flag changes. Optional; nil
	// disables emission.
	emit func(core.Event)

	// Evaluation instruments, resolved once by SetRecorder (never per call).
	// They only observe an evaluation; they never influence its result.
	evalCount map[string]obs.Counter
	evalDur   obs.Histogram
}

// Result label values for console_flag_evaluations_total{result}.
const (
	// ResultOn: the flag evaluated to an enabled Evaluation (rolled in).
	ResultOn = "on"
	// ResultOff: the flag evaluated to a disabled Evaluation (flag disabled,
	// out of scope, or excluded by rollout).
	ResultOff = "off"
	// ResultNotFound: no flag exists for the requested key.
	ResultNotFound = "not_found"
	// ResultError: evaluation failed for any other reason (e.g. store error).
	ResultError = "error"
)

// New returns an Engine backed by the given store. Evaluation metrics are
// discarded until SetRecorder is called.
func New(s store.FlagStore) *Engine {
	e := &Engine{store: s}
	e.SetRecorder(obs.Nop)
	return e
}

// SetRecorder installs the instrumentation Recorder and resolves the
// console_flag_evaluations_total{result} counters and the
// console_flag_eval_duration_seconds histogram once. Labels are the bounded
// result set only; flag and subject keys are never used as labels. A nil
// Recorder is treated as obs.Nop. Call before the engine is shared between
// goroutines (the composition root does so at construction).
func (e *Engine) SetRecorder(r obs.Recorder) {
	if r == nil {
		r = obs.Nop
	}
	e.evalCount = make(map[string]obs.Counter, 4)
	for _, res := range []string{ResultOn, ResultOff, ResultNotFound, ResultError} {
		e.evalCount[res] = r.Counter("console_flag_evaluations_total", obs.L("result", res))
	}
	e.evalDur = r.Histogram("console_flag_eval_duration_seconds")
}

// SetEmitter installs an event sink (e.g. a notify.Dispatcher's Emit). Passing
// nil disables emission.
func (e *Engine) SetEmitter(fn func(core.Event)) { e.emit = fn }

// emitFlagChange sends a flag_changed event when an emitter is installed.
func (e *Engine) emitFlagChange(key, message string) {
	if e.emit == nil {
		return
	}
	e.emit(core.Event{
		Type:    core.EventFlagChanged,
		Title:   "Flag " + key + " changed",
		Message: message,
		Flag:    key,
		At:      time.Now().UTC(),
	})
}

// Create persists a new flag.
func (e *Engine) Create(ctx context.Context, f core.Flag) error {
	if err := e.store.CreateFlag(ctx, f); err != nil {
		return err
	}
	e.emitFlagChange(f.Key, fmt.Sprintf("created (enabled=%t, scope=%s, rollout=%d%%)", f.Enabled, f.Scope, f.Rollout))
	return nil
}

// Get loads a flag by key.
func (e *Engine) Get(ctx context.Context, key string) (core.Flag, error) {
	return e.store.GetFlag(ctx, key)
}

// List returns all flags.
func (e *Engine) List(ctx context.Context) ([]core.Flag, error) {
	return e.store.ListFlags(ctx)
}

// Update persists changes to an existing flag.
func (e *Engine) Update(ctx context.Context, f core.Flag) error {
	if err := e.store.UpdateFlag(ctx, f); err != nil {
		return err
	}
	e.emitFlagChange(f.Key, fmt.Sprintf("updated (enabled=%t, scope=%s, rollout=%d%%)", f.Enabled, f.Scope, f.Rollout))
	return nil
}

// Delete removes a flag by key.
func (e *Engine) Delete(ctx context.Context, key string) error {
	if err := e.store.DeleteFlag(ctx, key); err != nil {
		return err
	}
	e.emitFlagChange(key, "deleted")
	return nil
}

// Evaluate resolves key for subj. The result is deterministic per (flag,
// subject): bucketing is derived from a stable hash, never from randomness, so
// repeated calls are identical.
//
// The pipeline is: load → enabled gate → scope gate → rollout gate → variant
// selection. The first gate that fails returns a disabled "off" Evaluation with
// a Reason naming the gate.
//
// Each call is recorded in console_flag_evaluations_total{result} (see the
// Result* constants) and console_flag_eval_duration_seconds; recording never
// affects the returned Evaluation.
func (e *Engine) Evaluate(ctx context.Context, key string, subj core.Subject) (core.Evaluation, error) {
	start := time.Now()
	ev, err := e.evaluate(ctx, key, subj)
	e.evalDur.Observe(time.Since(start).Seconds())
	switch {
	case err == nil && ev.Enabled:
		e.evalCount[ResultOn].Inc()
	case err == nil:
		e.evalCount[ResultOff].Inc()
	case errors.Is(err, core.ErrNotFound):
		e.evalCount[ResultNotFound].Inc()
	default:
		e.evalCount[ResultError].Inc()
	}
	return ev, err
}

// evaluate is the uninstrumented evaluation pipeline.
func (e *Engine) evaluate(ctx context.Context, key string, subj core.Subject) (core.Evaluation, error) {
	f, err := e.store.GetFlag(ctx, key)
	if err != nil {
		return core.Evaluation{}, err // store returns core.ErrNotFound when absent
	}

	if !f.Enabled {
		return core.Evaluation{FlagKey: f.Key, Enabled: false, Variant: "off", Reason: "flag_disabled"}, nil
	}

	if !inScope(f, subj) {
		return core.Evaluation{FlagKey: f.Key, Enabled: false, Variant: "off", Reason: "out_of_scope"}, nil
	}

	// Rollout gate: a stable bucket in [0,100). A 0% rollout includes nobody
	// (bucket >= 0 is always true); a 100% rollout includes everybody (bucket is
	// at most 99, always < 100).
	if bucket(f.Key+":"+subj.Key) >= f.Rollout {
		return core.Evaluation{FlagKey: f.Key, Enabled: false, Variant: "off", Reason: "rollout_excluded"}, nil
	}

	// Subject is rolled in. Multivariate flags pick a weighted variant from a
	// second, independent hash; boolean flags serve plain "on".
	if len(f.Variants) > 0 {
		v := pickVariant(f, subj)
		return core.Evaluation{FlagKey: f.Key, Enabled: true, Variant: v.Key, Value: v.Value, Reason: "variant"}, nil
	}

	return core.Evaluation{FlagKey: f.Key, Enabled: true, Variant: "on", Reason: "rollout_included"}, nil
}

// inScope reports whether subj falls within the flag's audience scope.
//
// Scope rules:
//   - ScopeAll: always in scope.
//   - ScopeBeta / ScopeAlpha: in scope when subj.Attributes["audience"] equals
//     the scope string ("beta"/"alpha"), OR when the attribute named after the
//     scope ("beta"/"alpha") equals "true".
//   - ScopeCohort: in scope when subj.Attributes["cohort"] == flag.Cohort.
//   - ScopeExperiment: treated like ScopeAll for the in/out gate — the
//     experiment linkage (Flag.Experiment) is metadata for analysis, not a gate.
//   - Any unknown scope: out of scope (fail closed).
func inScope(f core.Flag, subj core.Subject) bool {
	switch f.Scope {
	case core.ScopeAll, core.ScopeExperiment:
		return true
	case core.ScopeBeta, core.ScopeAlpha:
		audience := string(f.Scope)
		return subj.Attributes["audience"] == audience || subj.Attributes[audience] == "true"
	case core.ScopeCohort:
		return f.Cohort != "" && subj.Attributes["cohort"] == f.Cohort
	default:
		return false
	}
}

// pickVariant deterministically selects a weighted variant for subj using a
// hash independent of the rollout bucket. It maps a stable hash over the
// cumulative weights. If every weight is non-positive it falls back to the
// first variant, so a result is always returned.
func pickVariant(f core.Flag, subj core.Subject) core.Variant {
	total := 0
	for _, v := range f.Variants {
		if v.Weight > 0 {
			total += v.Weight
		}
	}
	if total <= 0 {
		return f.Variants[0]
	}

	point := int(hash64(f.Key+":variant:"+subj.Key) % uint64(total))
	cum := 0
	for _, v := range f.Variants {
		if v.Weight <= 0 {
			continue
		}
		cum += v.Weight
		if point < cum {
			return v
		}
	}
	return f.Variants[len(f.Variants)-1]
}

// bucket maps s to a stable integer in [0,100).
func bucket(s string) int {
	return int(hash64(s) % 100)
}

// hash64 returns the 64-bit FNV-1a hash of s.
func hash64(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
