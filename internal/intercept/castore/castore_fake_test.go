package castore

import (
	"context"
	"crypto/rand"
	"sync"
	"time"
)

// FakeInstaller is an in-memory Installer used by castore's own tests and by
// the contract tests of dependents that can't import a _test.go file (they
// declare their own fakes). It generates a real CA via GenerateCA on first
// EnsureCA and caches it, and tracks the Install/Uninstall counts so tests
// can assert on the lifecycle without touching the real trust store.
type FakeInstaller struct {
	// Now, when non-zero, seeds the CA's NotBefore. Zero falls back to
	// time.Now, which is fine for property-based assertions.
	Now time.Time

	mu            sync.Mutex
	ca            *CA
	ensured       int
	trustedSystem int
	uninstalled   int
	failWith      error
}

// Fail makes subsequent EnsureCA/Uninstall calls return err (still count the
// attempt). Optional: leave nil for a happy-path fake.
func (f *FakeInstaller) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failWith = err
}

// EnsuredCount returns how many times EnsureCA has been called.
func (f *FakeInstaller) EnsuredCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ensured
}

// UninstalledCount returns how many times Uninstall has been called.
func (f *FakeInstaller) UninstalledCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uninstalled
}

// TrustedSystemCount returns how many times TrustSystem has been called.
func (f *FakeInstaller) TrustedSystemCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.trustedSystem
}

// EnsureCA implements Installer.
func (f *FakeInstaller) EnsureCA(_ context.Context) (*CA, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensured++
	if f.failWith != nil {
		return nil, f.failWith
	}
	if f.ca == nil {
		now := f.Now
		if now.IsZero() {
			now = time.Now()
		}
		ca, err := GenerateCA(now, rand.Reader)
		if err != nil {
			return nil, err
		}
		f.ca = ca
	}
	return f.ca, nil
}

// TrustSystem implements Installer.
func (f *FakeInstaller) TrustSystem(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trustedSystem++
	if f.failWith != nil {
		return f.failWith
	}
	return nil
}

// Uninstall implements Installer.
func (f *FakeInstaller) Uninstall(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uninstalled++
	if f.failWith != nil {
		return f.failWith
	}
	return nil
}

// Compile-time guard: FakeInstaller satisfies the Installer seam.
var _ Installer = (*FakeInstaller)(nil)

// Guard that the stub used on unsupported OSes also satisfies Installer.
var _ Installer = unsupportedInstaller{}
