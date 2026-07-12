package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// IDGen is the seam used by any package that mints new identifiers (e.g.
// client_id, client_token, webhook ids). Injecting it keeps tests
// deterministic and avoids real randomness where assertions care about the
// exact string produced.
type IDGen interface {
	// NewID returns a fresh, unique identifier.
	NewID() string
}

// HexIDGen produces 16-byte (32 hex char) random ids using crypto/rand.
// Good enough for client tokens and row ids in the MVP; swap for a
// stricter generator later if needed.
type HexIDGen struct{}

// NewID implements IDGen.
func (HexIDGen) NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing on a dev workstation is exceptional enough
		// that panicking is the right call; we never want a silent empty id.
		panic("testutil: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// FakeIDGen returns a deterministic, monotonically increasing sequence so
// tests can assert on exact ids ("id-001", "id-002", ...).
type FakeIDGen struct {
	Counter int
}

// NewID implements IDGen.
func (f *FakeIDGen) NewID() string {
	f.Counter++
	return fmt.Sprintf("id-%03d", f.Counter)
}
