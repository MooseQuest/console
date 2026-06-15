package core

import "time"

// EventType classifies a notifiable change in Console.
type EventType string

const (
	// EventComponentDown is emitted when a component transitions to down.
	EventComponentDown EventType = "component_down"
	// EventComponentDegraded is emitted when a component transitions to degraded.
	EventComponentDegraded EventType = "component_degraded"
	// EventComponentRecovered is emitted when a component returns to operational
	// from a down or degraded state.
	EventComponentRecovered EventType = "component_recovered"
	// EventFlagChanged is emitted when a flag is created, updated, or deleted.
	EventFlagChanged EventType = "flag_changed"
)

// Event is a notifiable occurrence fanned out to Notifiers. It is intentionally
// flat so any sink (Slack, webhook, email) can render it without extra lookups.
type Event struct {
	Type    EventType `json:"type"`
	Title   string    `json:"title"`
	Message string    `json:"message,omitempty"`
	// Component is set for component_* events; Flag for flag_changed.
	Component string `json:"component,omitempty"`
	Flag      string `json:"flag,omitempty"`
	// From/To carry the health transition for component_* events.
	From HealthState `json:"from,omitempty"`
	To   HealthState `json:"to,omitempty"`
	At   time.Time   `json:"at"`
}

// Severity is a coarse importance hint for notifiers (e.g. message color).
func (t EventType) Severity() string {
	switch t {
	case EventComponentDown:
		return "critical"
	case EventComponentDegraded:
		return "warning"
	case EventComponentRecovered:
		return "good"
	default:
		return "info"
	}
}
