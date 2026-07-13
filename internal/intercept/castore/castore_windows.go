//go:build windows

package castore

// Windows CA installation (certutil -addstore Root) lands in Phase 6
// (Hardening). Until then a stub Installer keeps the cross-platform seam
// buildable on Windows so the rest of wiretap compiles and tests run.

// NewInstaller returns a stub Installer on Windows. EnsureCA/Uninstall both
// return ErrUnsupportedOS so callers can detect the unimplemented platform
// with errors.Is.
func NewInstaller(configDir string) Installer {
	return unsupportedInstaller{}
}
