// Package core defines Console's domain types: the vocabulary shared by every
// other package. It deliberately depends on nothing else in the project so it
// can be imported freely without creating cycles.
package core

import "time"

// Scope describes the audience a flag's rollout applies to. It mirrors the
// audience model from the original Console wireframes (Beta/Experiment/Cohort/
// Alpha/All) and is open-ended so plugins can introduce their own scopes.
type Scope string

const (
	ScopeAll        Scope = "all"        // every subject
	ScopeBeta       Scope = "beta"       // beta audience only
	ScopeAlpha      Scope = "alpha"      // a small invited set
	ScopeCohort     Scope = "cohort"     // a named cohort (see Flag.Cohort)
	ScopeExperiment Scope = "experiment" // tied to an experiment (see Flag.Experiment)
)

// Flag is a feature flag. The zero value is a disabled flag that serves its
// default variant to everyone.
type Flag struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Scope       Scope  `json:"scope"`
	// Rollout is the percentage of subjects in scope that receive the "on"
	// variant, 0..100. Evaluation is deterministic per (flag, subject).
	Rollout int `json:"rollout"`
	// Variants are the possible values the flag can serve. A boolean flag has
	// none and is evaluated as on/off via Rollout. A multivariate flag lists
	// its variants with relative weights.
	Variants []Variant `json:"variants,omitempty"`
	// Cohort names the cohort when Scope == ScopeCohort.
	Cohort string `json:"cohort,omitempty"`
	// Experiment names the linked experiment when Scope == ScopeExperiment.
	Experiment string `json:"experiment,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Variant is one possible value a multivariate flag can serve. Weight is the
// variant's relative share of in-scope, rolled-in subjects.
type Variant struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Weight int    `json:"weight"`
}

// Subject is the entity a flag is evaluated for — typically an end user, but it
// can be any keyed actor (a service, a tenant). Attributes carry context used
// by scope and cohort matching.
type Subject struct {
	Key        string            `json:"key"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Evaluation is the result of evaluating a flag for a subject.
type Evaluation struct {
	FlagKey string `json:"flag_key"`
	Enabled bool   `json:"enabled"`
	// Variant is the served variant key, or "on"/"off" for a boolean flag.
	Variant string `json:"variant"`
	Value   string `json:"value,omitempty"`
	// Reason explains the outcome (e.g. "flag_disabled", "out_of_scope",
	// "rollout", "default") — useful for debugging and the dashboard.
	Reason string `json:"reason"`
}
