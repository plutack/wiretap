//go:build linux

package castore

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// errNoCA signals that no persisted CA exists yet. Internal sentinel so
// EnsureCA can distinguish "first run" from real I/O errors.
var errNoCA = errors.New("castore: no persisted CA")

// LinuxInstaller persists the CA under ConfigDir. The system trust store
// install (TrustSystem) uses /usr/local/share/ca-certificates + update-ca-
// certificates (the Debian/Ubuntu mechanism; works on most desktop Linux).
// TrustSystem needs root; EnsureCA does not.
//
// LinuxInstaller is intentionally thin glue (file + one exec) and is not
// unit-tested: exercising the real system trust store requires root and a
// real distro. The pure crypto it calls (GenerateCA) is tested separately,
// and dependents exercise the Installer contract via the FakeInstaller in
// castore_fake_test.go.
type LinuxInstaller struct {
	// ConfigDir is the wiretap config directory (~/.config/wiretap). The CA
	// lives at <ConfigDir>/ca/wiretap-ca.crt + .key.
	ConfigDir string
}

// trustCertPath is the system path the update-ca-certificates tool scans for
// single-file additions. One file is plenty for wiretap's single root.
const trustCertPath = "/usr/local/share/ca-certificates/wiretap-ca.crt"

// EnsureCA implements Installer. It reuses a persisted CA when present,
// otherwise generates one and writes it under ConfigDir/ca. It does NOT
// install into the system trust store — call TrustSystem for that. This
// means `wiretap intercept start` works zero-touch without root: the shims
// pin the CA cert directly (--cacert, http.sslCAInfo, NODE_EXTRA_CA_CERTS,
// SSL_CERT_FILE), so the system trust store is only needed for tools that
// read it exclusively.
func (l *LinuxInstaller) EnsureCA(_ context.Context) (*CA, error) {
	caDir := filepath.Join(l.ConfigDir, "ca")
	if err := os.MkdirAll(caDir, 0o755); err != nil {
		return nil, fmt.Errorf("castore: mkdir %s: %w", caDir, err)
	}
	certPath := filepath.Join(caDir, "wiretap-ca.crt")
	keyPath := filepath.Join(caDir, "wiretap-ca.key")

	if ca, err := loadCA(certPath, keyPath); err == nil {
		return ca, nil
	} else if !errors.Is(err, errNoCA) {
		return nil, err
	}

	ca, err := GenerateCA(time.Now(), rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, ca.KeyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("castore: write CA key: %w", err)
	}
	if err := os.WriteFile(certPath, ca.CertPEM, 0o644); err != nil {
		return nil, fmt.Errorf("castore: write CA cert: %w", err)
	}
	return ca, nil
}

// TrustSystem implements Installer. It copies the persisted CA cert into
// /usr/local/share/ca-certificates/ and runs update-ca-certificates. Needs
// root. Optional: the three supported tools (curl, git, node) + Python/Go
// (via SSL_CERT_FILE) work without it. Call this once if you want other
// tools (e.g. a system Python without SSL_CERT_FILE) to trust the CA too.
func (l *LinuxInstaller) TrustSystem(ctx context.Context) error {
	certPath := filepath.Join(l.ConfigDir, "ca", "wiretap-ca.crt")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("castore: read CA cert %s: %w (run `wiretap intercept start` first to generate it)", certPath, err)
	}
	if err := os.WriteFile(trustCertPath, certPEM, 0o644); err != nil {
		return fmt.Errorf("castore: write trust store %s (need root?): %w", trustCertPath, err)
	}
	if err := runUpdateCA(ctx); err != nil {
		return fmt.Errorf("castore: %w", err)
	}
	return nil
}

// Uninstall implements Installer. It removes the system trust entry and
// refreshes the trust store; the persisted CA files under ConfigDir are left
// in place for cheap re-trust.
func (l *LinuxInstaller) Uninstall(ctx context.Context) error {
	if err := os.Remove(trustCertPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("castore: remove trust entry %s: %w", trustCertPath, err)
	}
	if err := runUpdateCA(ctx); err != nil {
		return fmt.Errorf("castore: %w", err)
	}
	return nil
}

// runUpdateCA invokes the distro's CA refresh command. Its output is intentionally
// discarded: it is loud on misconfigured systems and quiet on success, and we
// surface only a wrapped error.
func runUpdateCA(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "update-ca-certificates")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("update-ca-certificates: %w", err)
	}
	return nil
}

// loadCA reads and parses a persisted CA. Returns errNoCA when either file is
// missing so EnsureCA can treat that as "first run" rather than a failure.
func loadCA(certPath, keyPath string) (*CA, error) {
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errNoCA
		}
		return nil, fmt.Errorf("castore: read CA cert: %w", err)
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errNoCA
		}
		return nil, fmt.Errorf("castore: read CA key: %w", err)
	}

	certBlock, _ := pem.Decode(certBytes)
	if certBlock == nil {
		return nil, fmt.Errorf("castore: parse CA cert PEM: %w", errNoCA)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("castore: parse CA cert: %w", err)
	}

	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return nil, fmt.Errorf("castore: parse CA key PEM: %w", errNoCA)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("castore: parse CA key: %w", err)
	}
	return &CA{Cert: cert, Key: key, CertPEM: certBytes, KeyPEM: keyBytes}, nil
}

// NewInstaller returns the Linux Installer wired to persist under configDir.
// Non-Linux builds get an unsupportedInstaller from their own build-tag file.
func NewInstaller(configDir string) Installer {
	return &LinuxInstaller{ConfigDir: configDir}
}
