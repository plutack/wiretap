// Package castore owns the wiretap interception CA — the self-signed root
// certificate used to mint per-host leaf certificates for TLS interception —
// and the Installer seam that persists it on disk and augments the OS trust
// store.
//
// The pure crypto (GenerateCA / CA.LeafCert) lives in this build-tag-free
// file so it is unit-testable on every platform. The OS-specific trust-store
// mutation is split via build tags and surfaces ErrUnsupportedOS on platforms
// not yet implemented (darwin/windows initially), per PLAN.md §11.
//
// Consumer-side interfaces (Installer, CertSigner in the proxy package) keep
// production code decoupled from real trust stores and let tests substitute
// in-memory fakes — see internal/intercept/castore/castore_fake_test.go.
package castore

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"
)

// ErrUnsupportedOS is returned by stub Installers on platforms wiretap hasn't
// implemented CA installation for yet. Callers surface it to the user with a
// clear message; tests assert via errors.Is.
var ErrUnsupportedOS = errors.New("castore: unsupported OS for CA installation")

// caValidity is how long a generated wiretap CA is valid. Long enough to avoid
// churn, short enough to limit blast radius if the key leaks.
const caValidity = 10 * 365 * 24 * time.Hour

// leafValidity is how long a per-host leaf is valid. Short: leaves are minted
// on demand during interception and never persisted, so a longer validity
// would buy nothing.
const leafValidity = 365 * 24 * time.Hour

// CA is the wiretap interception root: a self-signed certificate plus its
// private key, used to sign per-host leaf certs on the fly during interception.
// The parsed objects drive signing; the PEM blocks drive persistence and
// trust store installation.
type CA struct {
	// Cert is the parsed root certificate.
	Cert *x509.Certificate
	// Key signs leaf certificates (ECDSA P-256).
	Key *ecdsa.PrivateKey
	// CertPEM is the PEM-encoded root cert (what trust stores ingest).
	CertPEM []byte
	// KeyPEM is the PEM-encoded ECDSA private key (persisted 0600).
	KeyPEM []byte
}

// GenerateCA mints a fresh self-signed ECDSA P-256 root CA. now controls
// NotBefore/NotAfter (injectable for deterministic tests); randReader drives
// serial numbers and ECDSA nonces (pass crypto/rand.Reader in production).
//
// The function is pure: no filesystem, no clock side effects. That keeps it
// trivially unit-testable on every platform.
func GenerateCA(now time.Time, randReader io.Reader) (*CA, error) {
	if randReader == nil {
		randReader = rand.Reader
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), randReader)
	if err != nil {
		return nil, fmt.Errorf("castore: generate CA key: %w", err)
	}

	serial, err := randSerial(randReader)
	if err != nil {
		return nil, err
	}

	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"wiretap"},
			CommonName:   "wiretap interception CA",
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(randReader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("castore: create CA cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("castore: parse CA cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("castore: marshal CA key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return &CA{Cert: cert, Key: key, CertPEM: certPEM, KeyPEM: keyPEM}, nil
}

// LeafCert mints a leaf certificate for host (a DNS name or IP literal) signed
// by the CA. The returned tls.Certificate carries the leaf and the CA so
// clients building a chain see the full path to the root. Leaves are never
// persisted; they are generated per CONNECT once and discarded after use.
func (c *CA) LeafCert(host string, now time.Time, randReader io.Reader) (tls.Certificate, error) {
	if randReader == nil {
		randReader = rand.Reader
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), randReader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("castore: generate leaf key: %w", err)
	}
	serial, err := randSerial(randReader)
	if err != nil {
		return tls.Certificate{}, err
	}

	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"wiretap"},
			CommonName:   host,
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(leafValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if ip := net.ParseIP(host); ip != nil {
		tpl.IPAddresses = []net.IP{ip}
	} else {
		tpl.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(randReader, tpl, c.Cert, &leafKey.PublicKey, c.Key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("castore: create leaf cert: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("castore: parse leaf cert: %w", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der, c.Cert.Raw},
		PrivateKey:  leafKey,
		Leaf:        leaf,
	}, nil
}

// randSerial returns a positive 128-bit random serial number, as recommended
// by RFC 5280 §4.1.2.2. The reader is overridable for deterministic tests.
func randSerial(r io.Reader) (*big.Int, error) {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	s, err := rand.Int(r, max)
	if err != nil {
		return nil, fmt.Errorf("castore: generate serial: %w", err)
	}
	return s, nil
}

// Installer is the seam for "make a CA exist and be trusted by this OS."
// Production impls are build-tag-split (Linux now, darwin/windows as stubs);
// tests substitute an in-memory FakeInstaller so dependents never touch the
// real trust store or need root.
//
// The interface is split into two concerns so `wiretap intercept start` never
// needs root: EnsureCA generates and persists the CA (user-writable config
// dir only), and TrustSystem installs it into the OS trust store (needs root
// on Linux). The shims pin the CA cert directly (--cacert, http.sslCAInfo,
// NODE_EXTRA_CA_CERTS, SSL_CERT_FILE), so TrustSystem is optional — it only
// matters for tools that read the system trust store exclusively.
type Installer interface {
	// EnsureCA returns the wiretap CA, generating and persisting it the first
	// time it is called. Does NOT install into the OS trust store — call
	// TrustSystem separately for that. Repeat calls return the same CA.
	EnsureCA(ctx context.Context) (*CA, error)
	// TrustSystem installs the persisted CA into the OS trust store so tools
	// that only read the system store (not env vars or --cacert flags) trust
	// wiretap's MITM certs. Needs root on Linux. Optional: the three supported
	// tools (curl, git, node) + Python/Go (via SSL_CERT_FILE) work without it.
	TrustSystem(ctx context.Context) error
	// Uninstall removes the CA from the OS trust store (best-effort). The
	// persisted CA files remain on disk so EnsureCA can re-trust cheaply.
	Uninstall(ctx context.Context) error
}

// unsupportedInstaller is the no-op Installer returned by NewInstaller on
// platforms wiretap hasn't implemented CA installation for yet. Every method
// returns ErrUnsupportedOS so callers can detect the case with errors.Is.
type unsupportedInstaller struct{}

// EnsureCA implements Installer.
func (unsupportedInstaller) EnsureCA(context.Context) (*CA, error) {
	return nil, ErrUnsupportedOS
}

// TrustSystem implements Installer.
func (unsupportedInstaller) TrustSystem(context.Context) error { return ErrUnsupportedOS }

// Uninstall implements Installer.
func (unsupportedInstaller) Uninstall(context.Context) error { return ErrUnsupportedOS }
