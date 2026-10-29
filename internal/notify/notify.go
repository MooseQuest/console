// Package notify is Console's alerting seam. A Notifier delivers a core.Event to
// an external sink (Slack, a webhook, email); the Dispatcher fans an event out
// to every registered Notifier. The flag and status engines emit events through
// a Dispatcher, so monitoring can actually reach a human.
package notify

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/obs"
)

// Notifier delivers an event to one destination. Implementations should be safe
// for concurrent use and return promptly; the Dispatcher bounds each call with a
// timeout but calls them on the path that triggered the event.
type Notifier interface {
	// Name identifies the sink, e.g. "slack".
	Name() string
	// Notify delivers ev. A returned error is logged, not propagated to the
	// triggering operation.
	Notify(ctx context.Context, ev core.Event) error
}

// defaultTimeout bounds a single Notifier delivery.
const defaultTimeout = 5 * time.Second

// Dispatcher fans events out to its Notifiers, best-effort: a slow or failing
// sink is bounded by a timeout and logged, and never fails the operation that
// produced the event. Its Emit method matches the engines' emitter hook.
type Dispatcher struct {
	mu        sync.RWMutex
	notifiers []Notifier
	timeout   time.Duration
	logf      func(string, ...any)
	rec       obs.Recorder
}

// NewDispatcher builds a Dispatcher with the given notifiers.
func NewDispatcher(notifiers ...Notifier) *Dispatcher {
	return &Dispatcher{
		notifiers: notifiers,
		timeout:   defaultTimeout,
		logf:      log.Printf,
		rec:       obs.Nop,
	}
}

// SetRecorder installs the instrumentation Recorder. Each delivery attempt
// increments console_notify_sent_total{sink,outcome}, where sink is
// Notifier.Name() and outcome is "ok" or "error". A nil Recorder is treated as
// obs.Nop. Only the notifier name is ever used as a label, never URLs or config.
func (d *Dispatcher) SetRecorder(r obs.Recorder) {
	if r == nil {
		r = obs.Nop
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rec = r
}

// Register adds a notifier.
func (d *Dispatcher) Register(n Notifier) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.notifiers = append(d.notifiers, n)
}

// Len reports how many notifiers are registered.
func (d *Dispatcher) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.notifiers)
}

// Emit delivers ev to every notifier synchronously, bounding each with the
// dispatcher timeout against a fresh background context (so a notification
// outlives the request that triggered it). Errors are logged, never returned.
func (d *Dispatcher) Emit(ev core.Event) {
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	d.mu.RLock()
	ns := make([]Notifier, len(d.notifiers))
	copy(ns, d.notifiers)
	rec := d.rec
	d.mu.RUnlock()

	for _, n := range ns {
		ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
		err := n.Notify(ctx, ev)
		outcome := "ok"
		if err != nil {
			outcome = "error"
			if d.logf != nil {
				d.logf("notify %s: %v", n.Name(), err)
			}
		}
		rec.Counter("console_notify_sent_total", obs.L("sink", n.Name()), obs.L("outcome", outcome)).Inc()
		cancel()
	}
}
