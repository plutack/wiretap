//go:build darwin

package castore

// Darwin CA installation lands in Phase 6 (Hardening). Until then a single
// stub Installer keeps the cross-platform seam buildable on macOS so the rest
// of wiretap compiles and tests run.

// NewInstaller returns a stub Installer on darwin. EnsureCA/Uninstall both
// return ErrUnsupportedOS so callers can detect the unimplemented platform
// with errors.Is.
func NewInstaller(configDir string) Installer {
	return unsupportedInstaller{}
}
