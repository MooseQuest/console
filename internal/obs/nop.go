package obs

import "context"

// Nop is the do-nothing Recorder used when observability is disabled. Every
// method is allocation-free (verified by tests).
var Nop Recorder = nop{}

type nop struct{}

func (nop) Counter(string, ...Label) Counter     { return nopInstrument{} }
func (nop) Histogram(string, ...Label) Histogram { return nopInstrument{} }
func (nop) Gauge(string, ...Label) Gauge         { return nopInstrument{} }

func (nop) StartSpan(ctx context.Context, _ string, _ ...Label) (context.Context, func(error)) {
	return ctx, nopEnd
}

func nopEnd(error) {}

// nopInstrument is a zero-size value implementing every instrument interface;
// converting it to an interface does not allocate.
type nopInstrument struct{}

func (nopInstrument) Inc()            {}
func (nopInstrument) Dec()            {}
func (nopInstrument) Add(float64)     {}
func (nopInstrument) Set(float64)     {}
func (nopInstrument) Observe(float64) {}
