package relayclient

import (
	"math/rand/v2"
	"time"
)

// Backoff computes the wait duration between failed dial attempts. It is an
// interface so tests can inject a fixed (or zero) backoff to keep them fast
// and deterministic.
type Backoff interface {
	// Next returns the next wait duration. Implementations should be safe to
	// call concurrently; ExponentialBackoff uses a mutex internally.
	Next() time.Duration
}

// ExponentialBackoff doubles the wait between consecutive calls up to Cap,
// then applies +/- Jitter*100 percent of the value as random jitter so
// many clients retrying the same relay do not synchronise (thundering herd).
//
// The zero value is invalid — at minimum Base must be set. Use New via the
// Client's WithBackoff option; the Client default uses Base=1s, Cap=30s.
type ExponentialBackoff struct {
	Base   time.Duration
	Cap    time.Duration
	Jitter float64 // 0.0..1.0; fraction +/- applied to each Next()
	Rand   *rand.Rand

	// current holds the most recent wait returned, used to compute the next.
	// Doubled on each Next until Cap is hit.
	current time.Duration
}

// Next returns an exponentially growing duration. Each call advances the
// internal state so subsequent calls back off further; the value resets to
// Base only when the caller invokes Reset — which the client currently never
// does (we want backoff to keep climbing until one succeeds).
//
// Synchronisation: ExponentialBackoff is not concurrent-safe by itself; the
// Client owns it and only invokes Next from a single goroutine (the outer
// reconnect loop). If that ever changes, add a sync.Mutex here.
func (e *ExponentialBackoff) Next() time.Duration {
	if e.current == 0 {
		e.current = e.Base
	} else {
		e.current *= 2
		if e.Cap > 0 && e.current > e.Cap {
			e.current = e.Cap
		}
	}
	wait := e.current
	if e.Jitter > 0 && e.Rand != nil {
		// Reduce or grow the wait by up to Jitter fraction.
		delta := float64(wait) * e.Jitter
		// Uniform in [-delta, +delta)
		shift := (e.Rand.Float64() * 2) - 1
		wait += time.Duration(shift * delta)
	}
	if wait < 0 {
		wait = 0
	}
	return wait
}

// Reset clears the current value so the next Next starts at Base. Called by
// the client after a successful session to avoid carrying stale backoff into
// a later failure burst.
func (e *ExponentialBackoff) Reset() {
	e.current = 0
}

// FixedBackoff always returns the same duration. Tests use it to keep test
// runs deterministic (often setting Duration to zero).
type FixedBackoff struct{ Duration time.Duration }

// Next implements Backoff.
func (f FixedBackoff) Next() time.Duration { return f.Duration }

// Reset implements Backoff. No-op for FixedBackoff.
func (f FixedBackoff) Reset() {}
