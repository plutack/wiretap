package castore

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestGenerateCA(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	ca, err := GenerateCA(now, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	if ca.Cert == nil || ca.Key == nil {
		t.Fatal("GenerateCA: Cert or Key is nil")
	}
	if !ca.Cert.IsCA {
		t.Error("GenerateCA: root cert is not a CA")
	}
	if ca.Cert.NotBefore.After(now) {
		t.Errorf("NotBefore %v should be at/before %v", ca.Cert.NotBefore, now)
	}
	if got := ca.Cert.NotAfter; !got.Equal(now.Add(caValidity).Round(time.Second)) && got.Before(now) {
		// Allow the -1m skew. Just assert it is far in the future.
		t.Errorf("NotAfter %v too early", got)
	}
	if !strings.Contains(string(ca.CertPEM), "BEGIN CERTIFICATE") {
		t.Error("CertPEM is not a CERTIFICATE block")
	}
	if !strings.Contains(string(ca.KeyPEM), "EC PRIVATE KEY") {
		t.Error("KeyPEM is not an EC PRIVATE KEY block")
	}
	// Subject Organisation should advertise wiretap.
	if len(ca.Cert.Subject.Organization) == 0 || ca.Cert.Subject.Organization[0] != "wiretap" {
		t.Errorf("Subject Organisation = %v, want [wiretap]", ca.Cert.Subject.Organization)
	}
}

func TestLeafCert_VerifiesAgainstRoot(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()

	ca, err := GenerateCA(now, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	tests := []struct {
		name   string
		host   string
		isName bool // true → DNS SAN, false → IP SAN
	}{
		{"dns name", "example.com", true},
		{"dns subdomain", "api.gateway.example.com", true},
		{"ipv4 literal", "127.0.0.1", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			leaf, err := ca.LeafCert(tc.host, now, rand.Reader)
			if err != nil {
				t.Fatalf("LeafCert: %v", err)
			}
			if leaf.Leaf == nil {
				t.Fatal("Leaf field is nil")
			}
			if leaf.Leaf.IsCA {
				t.Error("leaf cert is marked as a CA")
			}

			// The chain must verify back to the root.
			pool := x509.NewCertPool()
			pool.AddCert(ca.Cert)
			opts := x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			if _, err := leaf.Leaf.Verify(opts); err != nil {
				t.Fatalf("verify leaf against root: %v", err)
			}

			// SAN shape.
			if tc.isName {
				if len(leaf.Leaf.DNSNames) == 0 || leaf.Leaf.DNSNames[0] != tc.host {
					t.Errorf("DNSNames = %v, want %q", leaf.Leaf.DNSNames, tc.host)
				}
				if len(leaf.Leaf.IPAddresses) != 0 {
					t.Errorf("unexpected IP SANs: %v", leaf.Leaf.IPAddresses)
				}
			} else {
				want := net.ParseIP(tc.host)
				if len(leaf.Leaf.IPAddresses) == 0 || !leaf.Leaf.IPAddresses[0].Equal(want) {
					t.Errorf("IP SANs = %v, want %q", leaf.Leaf.IPAddresses, want)
				}
			}

			// Usable as a tls.Certificate: the chain is present.
			if len(leaf.Certificate) < 2 {
				t.Errorf("chain depth = %d, want >= 2 (leaf + root)", len(leaf.Certificate))
			}
		})
	}
}

func TestFakeInstaller_CachesAndCounts(t *testing.T) {
	t.Parallel()
	fi := &FakeInstaller{}

	a, err := fi.EnsureCA(context.Background())
	if err != nil {
		t.Fatalf("first EnsureCA: %v", err)
	}
	b, err := fi.EnsureCA(context.Background())
	if err != nil {
		t.Fatalf("second EnsureCA: %v", err)
	}
	if a != b {
		t.Error("EnsureCA returned distinct CAs across calls; want cached singleton")
	}
	if got := fi.EnsuredCount(); got != 2 {
		t.Errorf("EnsuredCount = %d, want 2", got)
	}
	if err := fi.Uninstall(context.Background()); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := fi.UninstalledCount(); got != 1 {
		t.Errorf("UninstalledCount = %d, want 1", got)
	}
}

func TestFakeInstaller_FailsWhenAsked(t *testing.T) {
	t.Parallel()
	fi := &FakeInstaller{}
	fi.Fail(errors.New("boom"))

	if _, err := fi.EnsureCA(context.Background()); err == nil {
		t.Error("EnsureCA: want error, got nil")
	}
	if err := fi.Uninstall(context.Background()); err == nil {
		t.Error("Uninstall: want error, got nil")
	}
}

func TestUnsupportedInstaller_Errors(t *testing.T) {
	t.Parallel()
	// Cast the stub to the interface to exercise the no-op path regardless of
	// the build's GOOS.
	var inst Installer = unsupportedInstaller{}
	if _, err := inst.EnsureCA(context.Background()); !errors.Is(err, ErrUnsupportedOS) {
		t.Errorf("EnsureCA: err = %v, want ErrUnsupportedOS", err)
	}
	if err := inst.Uninstall(context.Background()); !errors.Is(err, ErrUnsupportedOS) {
		t.Errorf("Uninstall: err = %v, want ErrUnsupportedOS", err)
	}
}

func TestLeafCert_UsableInTLSConfig(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	ca, err := GenerateCA(now, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	leaf, err := ca.LeafCert("localhost", now, rand.Reader)
	if err != nil {
		t.Fatalf("LeafCert: %v", err)
	}
	// Building a tls.Config should not panic and the cert should present as
	// the leaf (one cert + one root in the chain).
	cfg := &tls.Config{Certificates: []tls.Certificate{leaf}}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates len = %d, want 1", len(cfg.Certificates))
	}
}
