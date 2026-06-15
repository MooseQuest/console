package plugin

import (
	"time"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/plugin/proto"
)

// These helpers convert between core domain types and their proto wire forms.
// Times cross the wire as Unix nanoseconds (0 means the zero time); durations
// as raw nanoseconds (time.Duration is an int64 of nanoseconds).

func nanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}

func fromNanos(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

func flagToProto(f core.Flag) *proto.Flag {
	vs := make([]*proto.Variant, len(f.Variants))
	for i, v := range f.Variants {
		vs[i] = &proto.Variant{Key: v.Key, Value: v.Value, Weight: int32(v.Weight)}
	}
	return &proto.Flag{
		Key:               f.Key,
		Description:       f.Description,
		Enabled:           f.Enabled,
		Scope:             string(f.Scope),
		Rollout:           int32(f.Rollout),
		Variants:          vs,
		Cohort:            f.Cohort,
		Experiment:        f.Experiment,
		CreatedAtUnixNano: nanos(f.CreatedAt),
		UpdatedAtUnixNano: nanos(f.UpdatedAt),
	}
}

func flagFromProto(p *proto.Flag) core.Flag {
	vs := make([]core.Variant, len(p.Variants))
	for i, v := range p.Variants {
		vs[i] = core.Variant{Key: v.Key, Value: v.Value, Weight: int(v.Weight)}
	}
	return core.Flag{
		Key:         p.Key,
		Description: p.Description,
		Enabled:     p.Enabled,
		Scope:       core.Scope(p.Scope),
		Rollout:     int(p.Rollout),
		Variants:    vs,
		Cohort:      p.Cohort,
		Experiment:  p.Experiment,
		CreatedAt:   fromNanos(p.CreatedAtUnixNano),
		UpdatedAt:   fromNanos(p.UpdatedAtUnixNano),
	}
}

func componentToProto(c core.Component) *proto.Component {
	return &proto.Component{
		Key:               c.Key,
		Name:              c.Name,
		Description:       c.Description,
		Provider:          c.Provider,
		Config:            c.Config,
		CreatedAtUnixNano: nanos(c.CreatedAt),
		UpdatedAtUnixNano: nanos(c.UpdatedAt),
	}
}

func componentFromProto(p *proto.Component) core.Component {
	return core.Component{
		Key:         p.Key,
		Name:        p.Name,
		Description: p.Description,
		Provider:    p.Provider,
		Config:      p.Config,
		CreatedAt:   fromNanos(p.CreatedAtUnixNano),
		UpdatedAt:   fromNanos(p.UpdatedAtUnixNano),
	}
}

func checkToProto(c core.Check) *proto.Check {
	return &proto.Check{
		Component:         c.Component,
		State:             int32(c.State),
		Message:           c.Message,
		LatencyNanos:      int64(c.Latency),
		CheckedAtUnixNano: nanos(c.CheckedAt),
	}
}

func checkFromProto(p *proto.Check) core.Check {
	return core.Check{
		Component: p.Component,
		State:     core.HealthState(p.State),
		Message:   p.Message,
		Latency:   time.Duration(p.LatencyNanos),
		CheckedAt: fromNanos(p.CheckedAtUnixNano),
	}
}
