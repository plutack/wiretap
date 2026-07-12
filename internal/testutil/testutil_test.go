package testutil

import (
	"testing"
	"time"
)

// This file is deliberately a tiny, readable reference demonstrating the
// patterns the rest of the codebase should follow: table-driven subtests,
// t.Parallel for independent cases, t.Helper, deterministic doubles.

func TestFakeClock_NowAndAdvance(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_700_000_000, 0).UTC()
	clock := &FakeClock{T: start}

	if got := clock.Now(); !got.Equal(start) {
		t.Fatalf("Now = %v, want %v", got, start)
	}

	clock.Advance(5 * time.Second)
	want := start.Add(5 * time.Second)
	if got := clock.Now(); !got.Equal(want) {
		t.Errorf("after Advance, Now = %v, want %v", got, want)
	}
}

func TestFakeIDGen_NewID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{"first call", "id-001"},
		{"second call", "id-002"},
		{"third call", "id-003"},
	}
	// A single shared generator across subtests lets us assert the
	// sequence is monotonic — the whole point of FakeIDGen.
	gen := &FakeIDGen{}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Not parallel here: the sequence ordering is the thing under test.
			if got := gen.NewID(); got != tc.want {
				t.Errorf("NewID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSystemClock_ReturnsRealTime(t *testing.T) {
	t.Parallel()
	c := SystemClock{}
	before := time.Now()
	got := c.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("SystemClock.Now = %v, not within [%v, %v]", got, before, after)
	}
}

func TestHexIDGen_NewIDLength(t *testing.T) {
	t.Parallel()
	gen := HexIDGen{}
	id := gen.NewID()
	if len(id) != 32 {
		t.Fatalf("NewID length = %d, want 32 (16 bytes hex-encoded)", len(id))
	}
	// Two calls must not collide (this is a probabilistic check; a real
	// collision with crypto/rand would be astronomically unlikely).
	if gen.NewID() == id {
		t.Errorf("NewID returned a duplicate id %q", id)
	}
}
