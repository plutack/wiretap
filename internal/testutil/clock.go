// Package testutil holds small, dependency-free doubles (fake clock, fake
// id generator, golden-file helper) shared across the test suite.
//
// The intent is to keep tests deterministic without pulling in heavy
// mocking frameworks, and to keep the production code free of
// package-level mutable state (everything that needs time or ids gets a
// Clock or IDGen injected in).
package testutil

import "time"

// Clock is the seam used by any package that reads the current time.
// Production code injects SystemClock; tests inject FakeClock.
type Clock interface {
	// Now returns the current time as seen by this clock.
	Now() time.Time
}

// SystemClock reads the real wall clock.
type SystemClock struct{}

// Now implements Clock.
func (SystemClock) Now() time.Time { return time.Now() }

// FakeClock returns a controllable, deterministic time. Its zero value is
// the Unix epoch; tests typically construct it with a fixed start time and
// call Advance between actions.
type FakeClock struct {
	T time.Time
}

// Now implements Clock.
func (f *FakeClock) Now() time.Time { return f.T }

// Advance moves the fake clock forward by d. Tests use this to simulate
// the passage of time without sleeping.
func (f *FakeClock) Advance(d time.Duration) { f.T = f.T.Add(d) }
