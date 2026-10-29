package obs

import (
	"context"
	"time"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/status"
)

// Status-provider metric names.
const (
	// CheckDuration is the check latency histogram, labelled provider.
	CheckDuration = "console_check_duration_seconds"
	// CheckResults counts check outcomes, labelled provider and state.
	CheckResults = "console_check_results_total"
)

// Compile-time interface assertion.
var _ status.Provider = (*instrumentedProvider)(nil)

type instrumentedProvider struct {
	next status.Provider
	name string
	dur  Histogram
	// results is indexed by core.HealthState; resolved once.
	results [core.StateDown + 1]Counter
}

// WrapProvider returns a status.Provider that records check latency and result
// counts on rec, delegating to next. The provider label is next.Name() (the
// provider type, never the component ID) and the state label is the
// core.HealthState string. Name() is read once at wrap time. A state outside
// the core enum (e.g. from a plugin) is counted as unknown to keep label
// cardinality bounded.
func WrapProvider(next status.Provider, rec Recorder) status.Provider {
	if rec == nil {
		rec = Nop
	}
	p := &instrumentedProvider{next: next, name: next.Name()}
	p.dur = rec.Histogram(CheckDuration, L("provider", p.name))
	for st := core.StateUnknown; st <= core.StateDown; st++ {
		p.results[st] = rec.Counter(CheckResults, L("provider", p.name), L("state", st.String()))
	}
	return p
}

// Name implements status.Provider and returns the wrapped provider's name, so
// engine registration is unaffected by the wrapper.
func (p *instrumentedProvider) Name() string { return p.name }

// Check implements status.Provider.
func (p *instrumentedProvider) Check(ctx context.Context, comp core.Component) core.Check {
	start := time.Now()
	c := p.next.Check(ctx, comp)
	p.dur.Observe(time.Since(start).Seconds())
	st := c.State
	if st < core.StateUnknown || st > core.StateDown {
		st = core.StateUnknown
	}
	p.results[st].Inc()
	return c
}
