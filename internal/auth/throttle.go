package auth

import (
	"sync"
	"time"
)

// Login throttle parameters.
const (
	throttleFreeFailures = 3                // failures before any delay
	throttleBase         = 2 * time.Second  // first delay
	throttleMax          = 15 * time.Minute // delay cap
	throttleForget       = time.Hour        // idle time after which a client is forgotten
	// MaxThrottleClients bounds the throttle table.
	MaxThrottleClients = 10000
)

type attempt struct {
	fails int
	until time.Time // locked out until
	last  time.Time
}

// Throttle applies exponential backoff to failed logins per client key (the
// remote address). It is in memory and bounded; it is safe for concurrent use.
type Throttle struct {
	mu      sync.Mutex
	now     func() time.Time
	max     int
	clients map[string]*attempt
}

// NewThrottle returns an empty throttle. now may be nil (time.Now).
func NewThrottle(now func() time.Time) *Throttle {
	if now == nil {
		now = time.Now
	}
	return &Throttle{now: now, max: MaxThrottleClients, clients: map[string]*attempt{}}
}

// Check reports how long key must wait before trying again; zero means allowed.
func (t *Throttle) Check(key string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	a, ok := t.clients[key]
	if !ok {
		return 0
	}
	if d := a.until.Sub(t.now()); d > 0 {
		return d
	}
	return 0
}

// Fail records a failed attempt and returns the resulting lockout duration
// (zero while within the free failures). Delay doubles per failure, capped.
func (t *Throttle) Fail(key string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	a, ok := t.clients[key]
	if !ok {
		t.makeRoom(now)
		a = &attempt{}
		t.clients[key] = a
	}
	a.fails++
	a.last = now
	if a.fails <= throttleFreeFailures {
		return 0
	}
	d := throttleMax
	if n := a.fails - throttleFreeFailures - 1; n < 20 {
		if c := throttleBase << uint(n); c < throttleMax {
			d = c
		}
	}
	a.until = now.Add(d)
	return d
}

// Success forgets key.
func (t *Throttle) Success(key string) {
	t.mu.Lock()
	delete(t.clients, key)
	t.mu.Unlock()
}

// Len reports the number of tracked clients.
func (t *Throttle) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.clients)
}

// makeRoom keeps the table under its bound: drop idle entries, then if still
// full evict the least recently active one. Caller holds mu.
func (t *Throttle) makeRoom(now time.Time) {
	if len(t.clients) < t.max {
		return
	}
	for k, a := range t.clients {
		if now.Sub(a.last) > throttleForget && !a.until.After(now) {
			delete(t.clients, k)
		}
	}
	if len(t.clients) < t.max {
		return
	}
	var oldest string
	var ot time.Time
	for k, a := range t.clients {
		if oldest == "" || a.last.Before(ot) {
			oldest, ot = k, a.last
		}
	}
	delete(t.clients, oldest)
}
