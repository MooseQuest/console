// Package flags implements Console's feature-flag evaluation engine. It wraps a
// store.FlagStore for persistence and resolves a flag to an Evaluation for a
// given subject deterministically: the same (flag, subject) pair always yields
// the same result, so a rollout is stable across calls and processes.
package flags

import (
	"context"
	"hash/fnv"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/store"
)

// Engine evaluates feature flags and provides CRUD passthrough to its store.
type Engine struct {
	store store.FlagStore
}

// New returns an Engine backed by the given store.
func New(s store.FlagStore) *Engine {
	return &Engine{store: s}
}

// Create persists a new flag.
func (e *Engine) Create(ctx context.Context, f core.Flag) error {
	return e.store.CreateFlag(ctx, f)
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
	return e.store.UpdateFlag(ctx, f)
}

// Delete removes a flag by key.
func (e *Engine) Delete(ctx context.Context, key string) error {
	return e.store.DeleteFlag(ctx, key)
}

// Evaluate resolves key for subj. The result is deterministic per (flag,
// subject): bucketing is derived from a stable hash, never from randomness, so
// repeated calls are identical.
//
// The pipeline is: load → enabled gate → scope gate → rollout gate → variant
// selection. The first gate that fails returns a disabled "off" Evaluation with
// a Reason naming the gate.
func (e *Engine) Evaluate(ctx context.Context, key string, subj core.Subject) (core.Evaluation, error) {
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
