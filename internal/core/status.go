package core

import "time"

// HealthState is the coarse health of a monitored target. The ordering matters:
// higher states are worse, so aggregating a set of checks is a max().
type HealthState int

const (
	StateUnknown     HealthState = iota // not yet checked
	StateOperational                    // healthy
	StateDegraded                       // working but impaired
	StateDown                           // not working
)

func (s HealthState) String() string {
	switch s {
	case StateOperational:
		return "operational"
	case StateDegraded:
		return "degraded"
	case StateDown:
		return "down"
	default:
		return "unknown"
	}
}

// Component is a monitored part of an application — an API, a worker, a
// database. Its current health is the latest Check result for it.
type Component struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Provider names the StatusProvider plugin that checks this component.
	Provider string            `json:"provider"`
	Config   map[string]string `json:"config,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Check is a single point-in-time health observation for a component.
type Check struct {
	Component string        `json:"component"`
	State     HealthState   `json:"state"`
	Message   string        `json:"message,omitempty"`
	Latency   time.Duration `json:"latency,omitempty"`
	CheckedAt time.Time     `json:"checked_at"`
}

// Health is an aggregate snapshot across components, suitable for a status page
// or the dashboard overview.
type Health struct {
	State      HealthState `json:"state"`
	Components []Check     `json:"components"`
	CheckedAt  time.Time   `json:"checked_at"`
}
