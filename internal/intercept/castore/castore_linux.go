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

// LinuxInstaller persists the CA under ConfigDir. TrustSystem selects the
// native trust-store mechanism available on the host: Debian/Ubuntu's
// update-ca-certificates or p11-kit's update-ca-trust (used by Arch and
// Fedora-family systems). TrustSystem needs root; EnsureCA does not.
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

// These are the native anchor locations for the supported Linux trust-store
// implementations. One file is plenty for wiretap's single root.
const (
	debianTrustCertPath = "/usr/local/share/ca-certificates/wiretap-ca.crt"
	p11KitTrustCertPath = "/etc/ca-certificates/trust-source/anchors/wiretap-ca.crt"
)

type linuxTrustStore struct {
	certPath string
	command  string
	args     []string
}

func resolveLinuxTrustStore() (linuxTrustStore, error) {
	if _, err := exec.LookPath("update-ca-certificates"); err == nil {
		return linuxTrustStore{
			certPath: debianTrustCertPath,
			command:  "update-ca-certificates",
		}, nil
	}
	if _, err := exec.LookPath("update-ca-trust"); err == nil {
		return linuxTrustStore{
			certPath: p11KitTrustCertPath,
			command:  "update-ca-trust",
			args:     []string{"extract"},
		}, nil
	}
	return linuxTrustStore{}, fmt.Errorf("castore: no supported Linux trust-store updater found (need update-ca-certificates or update-ca-trust)")
}

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

// TrustSystem implements Installer. It copies the persisted CA cert into the
// native Linux trust anchor directory and refreshes the system trust store.
// Needs root. Optional: the three supported tools (curl, git, node) + Python/Go
// (via SSL_CERT_FILE) work without it.
func (l *LinuxInstaller) TrustSystem(ctx context.Context) error {
	certPath := filepath.Join(l.ConfigDir, "ca", "wiretap-ca.crt")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("castore: read CA cert %s: %w (run `wiretap intercept start` first to generate it)", certPath, err)
	}
	store, err := resolveLinuxTrustStore()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(store.certPath), 0o755); err != nil {
		return fmt.Errorf("castore: create trust store directory %s (need root?): %w", filepath.Dir(store.certPath), err)
	}
	if err := os.WriteFile(store.certPath, certPEM, 0o644); err != nil {
		return fmt.Errorf("castore: write trust store %s (need root?): %w", store.certPath, err)
	}
	if err := runUpdateCA(ctx, store); err != nil {
		return fmt.Errorf("castore: %w", err)
	}
	return nil
}

// Uninstall implements Installer. It removes wiretap's anchor from every
// supported trust-store location — not just the one the host resolves to
// today, because the available updater can change between TrustSystem and
// Uninstall (e.g. distro reinstall or migration), and a stale anchor would
// leave a trusted wiretap root behind. Missing entries are fine. With no
// updater installed nothing could have been trusted system-wide in the
// first place, so uninstall succeeds as a no-op. The persisted CA files
// under ConfigDir are left in place for cheap re-trust.
func (l *LinuxInstaller) Uninstall(ctx context.Context) error {
	for _, path := range []string{debianTrustCertPath, p11KitTrustCertPath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("castore: remove trust entry %s: %w", path, err)
		}
	}
	return refreshTrustStores(ctx)
}

// refreshTrustStores runs every installed trust-store updater so anchor
// additions and removals propagate regardless of which mechanism the host
// uses. Debian-family systems ship both tools; running both is harmless.
func refreshTrustStores(ctx context.Context) error {
	for _, store := range []linuxTrustStore{
		{command: "update-ca-certificates"},
		{command: "update-ca-trust", args: []string{"extract"}},
	} {
		if _, err := exec.LookPath(store.command); err != nil {
			continue
		}
		if err := runUpdateCA(ctx, store); err != nil {
			return fmt.Errorf("castore: %w", err)
		}
	}
	return nil
}

// runUpdateCA invokes the distro's CA refresh command. Its output is intentionally
// discarded: it is loud on misconfigured systems and quiet on success, and we
// surface only a wrapped error.
func runUpdateCA(ctx context.Context, store linuxTrustStore) error {
	cmd := exec.CommandContext(ctx, store.command, store.args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", store.command, err)
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
